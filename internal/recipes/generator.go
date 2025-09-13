package recipes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/samber/lo"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/history"
	"careme/internal/kroger"
	"careme/internal/locations"
)

func Hash(location string, date time.Time) string {
	return fmt.Sprintf("%x", location+date.Format("2006-01-02"))
}

type Generator struct {
	config         *config.Config
	aiClient       *ai.Client
	krogerClient   kroger.ClientWithResponsesInterface //probably need only subset
	historyStorage *history.HistoryStorage
	inFlight       map[string]struct{}
	generationLock sync.Mutex
}

type GeneratedRecipes struct {
	Recipes []history.Recipe `json:"recipes"`
}

func NewGenerator(cfg *config.Config) (*Generator, error) {
	client, err := kroger.FromConfig(context.TODO(), cfg)
	if err != nil {
		return nil, err
	}
	return &Generator{
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
	Location        *locations.Location
	Date            time.Time
	Staples         []filter
	PreviousRecipes []string
	People          int
	Instructions    string
}

func DefaultParams(l *locations.Location) *generatorParams {
	return &generatorParams{
		Date:     time.Now(), // shave time
		Location: l,
		People:   2,
		Staples:  DefaultStaples(),
	}
}

func (g *generatorParams) Hash() string {
	bytes, err := json.Marshal(g)
	if err != nil {
		panic(err)
	}
	fnv := fnv.New32a()
	_, err = fnv.Write(bytes)
	if err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(fnv.Sum(nil))
}

func (g *generatorParams) Exclude(staple []string) {
	g.Staples = lo.Filter(g.Staples, func(f filter, _ int) bool {
		return !slices.Contains(staple, f.Term)
	})
}

func DefaultStaples() []filter {
	return []filter{
		{
			Term: "beef",
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms"},
		},
		{
			Term: "fish",
		},
		{
			Term: "pork",
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
	log.Printf("Generating recipes for location: %s", p.Location)

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

	//
	response, err := g.aiClient.GenerateRecipes(p.Location, ingredients, p.Instructions, p.Date)
	if err != nil {
		return "", fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	w, err := os.OpenFile("recipes/"+hash+".txt", os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open recipe file: %w", err)
	}
	defer w.Close()

	if _, err := w.WriteString(response); err != nil {
		return "", fmt.Errorf("failed to write recipe file: %w", err)
	}

	log.Printf("generated chat for %s in %s, stored in recipes/%s.txt", p.Location.ID, time.Since(start), hash)
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
	Term   string
	Brands []string
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(p *generatorParams) ([]string, error) {

	var ingredients []string
	for _, category := range p.Staples {
		cingredients, err := g.GetIngredients(p.Location.ID, category, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to get ingredients: %w", err)
		}
		ingredients = append(ingredients, cingredients...)
		log.Printf("Found %d ingredients for category: %s", len(cingredients), category.Term)
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
		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) {
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
				"%s %s price %.2f",
				//toStr(product.Brand),
				toStr(product.Description),
				toStr(item.Size),
				toFloat32(item.Price.Regular),
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
