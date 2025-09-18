package recipes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/history"
	"careme/internal/kroger"
	"careme/internal/locations"
)

type Generator struct {
	config         *config.Config
	aiClient       *ai.Client
	krogerClient   kroger.ClientWithResponsesInterface //probably need only subset
	cache          cache.Cache
	historyStorage *history.HistoryStorage
	inFlight       map[string]struct{}
	generationLock sync.Mutex
}

type GeneratedRecipes struct {
	Recipes []history.Recipe `json:"recipes"`
}

func NewGenerator(cfg *config.Config, cache cache.Cache) (*Generator, error) {
	client, err := kroger.FromConfig(context.TODO(), cfg)
	if err != nil {
		return nil, err
	}
	return &Generator{
		cache:          cache, //should this also pull from config?
		config:         cfg,
		aiClient:       ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model),
		krogerClient:   client,
		historyStorage: history.NewHistoryStorage(cfg.History.StoragePath, cfg.History.RetentionDays),
		inFlight:       make(map[string]struct{}),
	}, nil
}

// eventually we want to use  blob with exipiry for this
func (g *Generator) isGenerating(hash string) (bool, func()) {
	g.generationLock.Lock()
	defer g.generationLock.Unlock()
	if _, exists := g.inFlight[hash]; exists {
		return true, nil
	}
	g.inFlight[hash] = struct{}{}
	return false, func() {
		g.generationLock.Lock()
		defer g.generationLock.Unlock()
		delete(g.inFlight, hash)
	}
}

type generatorParams struct {
	Location *locations.Location `json:"location,omitempty"`
	Date     time.Time           `json:"date,omitempty"`
	Staples  []filter            `json:"staples,omitempty"`
	//PreviousRecipes []string
	//People       int
	Instructions string `json:"instructions,omitempty"`
}

func DefaultParams(l *locations.Location, date time.Time) *generatorParams {

	// normalize to midnight (shave hours, minutes, seconds, nanoseconds)
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return &generatorParams{
		Date:     date, // shave time
		Location: l,
		//People:   2,
		Staples: DefaultStaples(),
	}
}

func (g *generatorParams) String() string {
	return fmt.Sprintf("%s on %s", g.Location.ID, g.Date.Format("2006-01-02"))
}

func (g *generatorParams) Hash() string {
	bytes, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	fnv := fnv.New64a()
	_, err = fnv.Write(bytes)
	if err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(fnv.Sum([]byte("recipe")))
}

// so far just excludes instructions. Can exclude people and other things
func (g *generatorParams) LocationHash() string {

	fnv := fnv.New64a()
	lo.Must(fnv.Write([]byte(g.Location.ID)))
	lo.Must(fnv.Write([]byte(g.Date.Format("2006-01-02"))))
	bytes := lo.Must(json.Marshal(g.Staples))
	lo.Must(fnv.Write(bytes))
	return base64.URLEncoding.EncodeToString(fnv.Sum([]byte("ingredients")))

}

func DefaultStaples() []filter {
	return []filter{
		{
			Term: "beef",
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms", "Draper Valley"}, //"Simple Truth"? do these vary in every state?
		},
		{
			Term: "fish",
		},
		{
			Term: "pork", //Kroger?
		},
		{
			Term: "shellfish",
		},
		{
			Term:   "lamb",
			Brands: []string{"Simple Truth"},
		},
		{
			Term: "produce vegetable",
		},
	}
}

func (g *Generator) GenerateRecipes(p *generatorParams) (string, error) {
	log.Printf("Generating recipes for location: %s", p)

	/*previousRecipes, err := g.getPreviousRecipes()
	if err != nil {
		log.Printf("Warning: Could not fetch recipe history: %v", err)
		previousRecipes = []string{}
	}*/

	hash := p.Hash()
	generating, done := g.isGenerating(hash)
	if generating {
		log.Printf("Generation already in progress for %s, skipping", hash)
		return "", nil
	}
	defer done()
	start := time.Now()

	ingredients, err := g.GetStaples(p)
	if err != nil {
		return "", fmt.Errorf("failed to get staples: %w", err)
	}

	//log.Printf("Found %d sale ingredients, %d previous recipes", 		len(ingredients), len(previousRecipes))

	response, err := g.aiClient.GenerateRecipes(p.Location, ingredients, p.Instructions, p.Date)
	if err != nil {
		return "", fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	log.Printf("generated chat for %s in %s stored in recipes/%s", p, time.Since(start), hash)

	if err := g.cache.Set(p.Hash(), response); err != nil {
		log.Printf("failed to cache recipe for %s: %v", p, err)
		return response, err
	}

	return response, nil

	/*recipes, err := g.parseAIResponse(response, location)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	if err := g.historyStorage.SaveRecipes(recipes); err != nil {
		log.Printf("Warning: Could not save recipes to history: %v", err)
	}
	*/

	//return recipes, nil
}

type filter struct {
	Term   string   `json:"term,omitempty"`
	Brands []string `json:"brands,omitempty"`
}

func Filter(term string, brands []string) filter {
	return filter{
		Term:   term,
		Brands: brands,
	}
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(p *generatorParams) ([]string, error) {

	lochash := p.LocationHash()
	if ingredientblob, found := g.cache.Get(lochash); found {
		log.Printf("serving cached ingredients for %s: %s", p.String(), lochash)
		return strings.Split(ingredientblob, "\n"), nil
	}

	var ingredients []string
	var errors []error
	var wg sync.WaitGroup
	var lock sync.Mutex
	wg.Add(len(p.Staples))
	for _, category := range p.Staples {
		go func(category filter) {
			defer wg.Done()

			cingredients, err := g.GetIngredients(p.Location.ID, category, 0)
			lock.Lock()
			defer lock.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to get ingredients: %w", err))
				return
			}
			ingredients = append(ingredients, cingredients...)
			log.Printf("Found %d ingredients for category: %s at %s", len(cingredients), category.Term, p.Location.ID)
		}(category)
	}

	wg.Wait()

	if err := g.cache.Set(p.LocationHash(), strings.Join(ingredients, "\n")); err != nil {
		log.Printf("failed to cache ingredients for %s: %v", p, err)
		return nil, err
	}
	return ingredients, nil
}

// move to krogrer client as everyone will be differnt here?
func (g *Generator) GetIngredients(location string, f filter, skip int) ([]string, error) {
	limit := 50
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	//brand := "empty" doesn't work have to check for nil
	//fulfillment := "ais" drmatically shortens?
	products, err := g.krogerClient.ProductSearchWithResponse(context.TODO(), &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &f.Term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
		//FilterBrand:      &brand,
		//FilterFulfillment: &fulfillment,
	})
	if err != nil {
		return nil, fmt.Errorf("failed on product searchwith response %w", err)
	}

	if products.StatusCode() != http.StatusOK {
		fmt.Printf("Kroger ProductSearchWithResponse returned status: %d\n", products.StatusCode())
		return nil, fmt.Errorf("got %d code from kroger", products.StatusCode())
	}

	var ingredients []string

	for _, product := range *products.JSON200.Data {
		wildcard := len(f.Brands) > 0 && f.Brands[0] == "*"

		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) && !wildcard {
			continue
		}
		//end up with a bunch of frozen chicken with out this.
		if slices.Contains(*product.Categories, "Frozen") {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				// todo what does this mean?
				continue
			}

			//does just giving the model json work better here?
			ingredient := fmt.Sprintf(
				"%s %s price %.2f", //				"%s, %s %s price %.2f %s",
				//toStr(product.Brand),
				toStr(product.Description),
				toStr(item.Size),
				toFloat32(item.Price.Regular),
				//strings.Join(*product.Categories, ", "),
			)

			if toFloat32(item.Price.Promo) > 0.0 {
				ingredient += fmt.Sprintf(" sale %.2f", toFloat32(item.Price.Promo))
			}
			ingredients = append(ingredients, ingredient)
			//strings.Join(*product.Categories, ", "),

		}
	}

	//recursion is pretty dumb pagination
	if len(*products.JSON200.Data) == limit && skip+limit < 250 { //fence post error
		page, err := g.GetIngredients(location, f, skip+limit)
		if err != nil {
			return nil, nil
		}
		ingredients = append(ingredients, page...)
	}

	return ingredients, nil
}

// toStr returns the string value if non-nil, or "empty" otherwise.
func toStr(s *string) string {
	if s == nil {
		return "empty"
	}
	return *s
}

// toFloat32 returns the float32 value if non-nil, or 0.0 otherwise.
func toFloat32(f *float32) float32 {
	if f == nil {
		return 0.0
	}
	return *f
}

func (g *Generator) getPreviousRecipes() ([]string, error) {
	return g.historyStorage.GetRecipeNames(14) // Last 2 weeks
}

func (g *Generator) parseAIResponse(response, location string) ([]history.Recipe, error) {
	var generatedRecipes GeneratedRecipes
	if err := json.Unmarshal([]byte(response), &generatedRecipes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal AI response: %w", err)
	}

	if len(generatedRecipes.Recipes) != 4 {
		return nil, fmt.Errorf("expected 4 recipes, got %d", len(generatedRecipes.Recipes))
	}

	var recipes []history.Recipe
	for i, recipe := range generatedRecipes.Recipes {
		recipe.ID = fmt.Sprintf("recipe_%d", i+1)
		recipe.Location = location
		recipes = append(recipes, recipe)
	}

	return recipes, nil
}
