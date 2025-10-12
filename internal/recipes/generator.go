package recipes

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
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
	"careme/internal/kroger"
	"careme/internal/locations"
)

type Generator struct {
	config         *config.Config
	aiClient       *ai.Client
	krogerClient   kroger.ClientWithResponsesInterface //probably need only subset
	cache          cache.Cache
	inFlight       map[string]struct{}
	generationLock sync.Mutex
}

func NewGenerator(cfg *config.Config, cache cache.Cache) (*Generator, error) {
	client, err := kroger.FromConfig(context.TODO(), cfg)
	if err != nil {
		return nil, err
	}
	return &Generator{
		cache:        cache, //should this also pull from config?
		config:       cfg,
		aiClient:     ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model),
		krogerClient: client,
		inFlight:     make(map[string]struct{}),
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
	//People       int
	Instructions string   `json:"instructions,omitempty"`
	LastRecipes  []string `json:"last_recipes,omitempty"`
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
	fnv := fnv.New64a()
	lo.Must(fnv.Write([]byte(g.Location.ID)))
	lo.Must(fnv.Write([]byte(g.Date.Format("2006-01-02"))))
	bytes := lo.Must(json.Marshal(g.Staples))
	lo.Must(fnv.Write(bytes))
	lo.Must(fnv.Write([]byte(g.Instructions)))
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

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) (string, error) {
	slog.Info("Generating recipes for location", "location", p.String())

	/*previousRecipes, err := g.getPreviousRecipes()
	if err != nil {
		slog.Warn("Warning: Could not fetch recipe history", "error", err)
		previousRecipes = []string{}
	}*/

	hash := p.Hash()
	generating, done := g.isGenerating(hash)
	if generating {
		slog.InfoContext(ctx, "Generation already in progress, skipping", "hash", hash)
		return "", nil
	}
	defer done()
	start := time.Now()

	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return "", fmt.Errorf("failed to get staples: %w", err)
	}

	response, err := g.aiClient.GenerateRecipes(p.Location, ingredients, p.Instructions, p.Date, p.LastRecipes)
	if err != nil {
		return "", fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)

	if err := g.cache.Set(p.Hash(), response); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe", "location", p.String(), "error", err)
		return response, err
	}

	// Also cache the params for hash-based retrieval
	paramsJSON := lo.Must(json.Marshal(p))

	if err := g.cache.Set(p.Hash()+".params", string(paramsJSON)); err != nil {
		slog.ErrorContext(ctx, "failed to cache params", "location", p.String(), "error", err)
	}

	return response, nil
}

// LoadParamsFromHash loads generator params from cache using the hash
func (g *Generator) LoadParamsFromHash(hash string) (*generatorParams, error) {
	paramsReader, err := g.cache.Get(hash + ".params")
	if err != nil {
		return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
	}
	defer paramsReader.Close()

	var params generatorParams
	if err := json.NewDecoder(paramsReader).Decode(&params); err != nil {
		return nil, fmt.Errorf("failed to decode params: %w", err)
	}
	return &params, nil
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
func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]string, error) {

	lochash := p.LocationHash()
	var ingredients []string

	if ingredientblob, err := g.cache.Get(lochash); err == nil {
		slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash)
		defer ingredientblob.Close()
		sc := bufio.NewScanner(ingredientblob)
		for sc.Scan() {
			ingredients = append(ingredients, sc.Text())
		}
		if err := sc.Err(); err != nil {
			slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
			return nil, err
		}
		return ingredients, nil
	}

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
			slog.InfoContext(ctx, "Found ingredients for category", "count", len(cingredients), "category", category.Term, "location", p.Location.ID)
		}(category)
	}

	wg.Wait()

	if err := g.cache.Set(p.LocationHash(), strings.Join(ingredients, "\n")); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
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
