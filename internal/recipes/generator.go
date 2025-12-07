package recipes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/samber/lo"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
}

type Generator struct {
	config         *config.Config
	aiClient       aiClient
	krogerClient   kroger.ClientWithResponsesInterface //probably need only subset
	cache          cache.Cache
	inFlight       map[string]struct{}
	generationLock sync.Mutex
}

func NewGenerator(cfg *config.Config, cache cache.Cache) (*Generator, error) {
	client, err := kroger.FromConfig(cfg)
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
	UserID       string   `json:"user_id,omitempty"`
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
			Term:   "beef",
			Brands: []string{"Simple Truth", "Kroger"},
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms", "Draper Valley", "Simple Truth"}, //"Simple Truth"? do these vary in every state?
		},
		{
			Term: "fish",
		},
		{
			Term:   "pork", //Kroger?
			Brands: []string{"PORK", "Kroger", "Harris Teeter"},
		},
		{
			Term:   "shellfish",
			Brands: []string{"Sand Bar", "Kroger"},
			Frozen: true, //remove after 500 sadness?
		},
		{
			Term:   "lamb",
			Brands: []string{"Simple Truth"},
		},
		{
			Term:   "produce vegetable",
			Brands: []string{"*"}, //ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
	}
}

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) error {
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())

	hash := p.Hash()
	generating, done := g.isGenerating(hash)
	if generating {
		slog.InfoContext(ctx, "Generation already in progress, skipping", "hash", hash)
		return nil
	}
	defer done()
	start := time.Now()

	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return fmt.Errorf("failed to get staples: %w", err)
	}

	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, p.Instructions, p.Date, p.LastRecipes)
	if err != nil {
		return fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)

	// Save each recipe separately by its hash
	for _, recipe := range shoppingList.Recipes {
		recipe.OriginHash = p.Hash()
		recipeJSON := lo.Must(json.Marshal(recipe))
		if err := g.cache.Set("recipe/"+recipe.ComputeHash(), string(recipeJSON)); err != nil {
			slog.ErrorContext(ctx, "failed to cache individual recipe", "recipe", recipe.Title, "error", err)
			return err
		}
	}
	//we could actually nuke out the rest of recipe and lazily load but not yet
	shoppingJSON := lo.Must(json.Marshal(shoppingList))
	if err := g.cache.Set(p.Hash(), string(shoppingJSON)); err != nil {
		slog.ErrorContext(ctx, "failed to cache shopping list document", "location", p.String(), "error", err)
		return err
	}

	// Also cache the params for hash-based retrieval
	// TODO: Consider embedding the params directly in the shoppingList structure.
	// This would allow us to cache both the shopping list and its associated parameters together,
	// avoiding the need for a separate cache entry for params (currently stored as "<hash>.params").
	// Embedding params could simplify cache management and ensure all relevant data is retrieved together.
	paramsJSON := lo.Must(json.Marshal(p))
	if err := g.cache.Set(p.Hash()+".params", string(paramsJSON)); err != nil {
		slog.ErrorContext(ctx, "failed to cache params", "location", p.String(), "error", err)
		return err
	}

	return nil
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
	Frozen bool     `json:"frozen,omitempty"`
}

func Filter(term string, brands []string, frozen bool) filter {
	return filter{
		Term:   term,
		Brands: brands,
		Frozen: frozen,
	}
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]kroger.Ingredient, error) {

	lochash := p.LocationHash()
	var ingredients []kroger.Ingredient

	if ingredientblob, err := g.cache.Get(lochash); err == nil {
		defer ingredientblob.Close()
		jsonReader := json.NewDecoder(ingredientblob)
		if err := jsonReader.Decode(&ingredients); err == nil {
			slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(ingredients))
			return ingredients, nil
		}
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	var wg sync.WaitGroup
	var lock sync.Mutex
	wg.Add(len(p.Staples))
	for _, category := range p.Staples {
		go func(category filter) {
			defer wg.Done()
			cingredients, err := g.GetIngredients(ctx, p.Location.ID, category, 0)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get ingredients", "category", category.Term, "location", p.Location.ID, "error", err)
				return
			}
			lock.Lock()
			defer lock.Unlock()
			ingredients = append(ingredients, cingredients...)
			slog.InfoContext(ctx, "Found ingredients for category", "count", len(cingredients), "category", category.Term, "location", p.Location.ID)

		}(category)
	}

	wg.Wait()

	allingredientsJSON, err := json.Marshal(ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	if err := g.cache.Set(p.LocationHash(), string(allingredientsJSON)); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return ingredients, nil
}

// move to krogrer client as everyone will be differnt here?
func (g *Generator) GetIngredients(ctx context.Context, location string, f filter, skip int) ([]kroger.Ingredient, error) {
	limit := 25
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	//brand := "empty" doesn't work have to check for nil
	//fulfillment := "ais" drmatically shortens?
	//wrapped this in a retry and it did nothng
	products, err := g.krogerClient.ProductSearchWithResponse(context.TODO(), &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &f.Term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
		//FilterBrand:      &brand,
		//FilterFulfillment: &fulfillment,
	})
	if err != nil {
		return nil, fmt.Errorf("kroger product search request failed: %w", err)
	}
	if products.StatusCode() != http.StatusOK {
		output, _ := json.Marshal(products.JSON500) //handle other errors?
		return nil, fmt.Errorf("got %d code from kroger : %s", products.StatusCode(), string(output))
	}

	var ingredients []kroger.Ingredient

	for _, product := range *products.JSON200.Data {
		wildcard := len(f.Brands) > 0 && f.Brands[0] == "*"

		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) && !wildcard {
			continue
		}
		//end up with a bunch of frozen chicken with out this.
		if slices.Contains(*product.Categories, "Frozen") && !f.Frozen {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				// todo what does this mean?
				continue
			}

			//does just giving the model json work better here?
			ingredient := kroger.Ingredient{
				Brand:        product.Brand,
				Description:  product.Description,
				Size:         item.Size,
				PriceRegular: item.Price.Regular,
				PriceSale:    item.Price.Promo,
				//CountryOrigin: product.CountryOrigin,
				//AisleNumber:   product.AisleLocations[0].Number,
				//Favorite: item.Favorite,
				//InventoryStockLevel: item.InventoryStockLevel),
			}

			/*if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
				ingredient.AisleNumber = (*product.AisleLocations)[0].Number
			}*/

			ingredients = append(ingredients, ingredient)
			//strings.Join(*product.Categories, ", "),

		}
	}

	//Debug level?
	//slog.InfoContext(ctx, "got", "ingredients", len(ingredients), "products", len(*products.JSON200.Data), "term", f.Term, "brands", f.Brands, "location", location, "skip", skip)

	//recursion is pretty dumb pagination
	//500's seem gone.
	if len(*products.JSON200.Data) == limit && skip+limit < 100 { //fence post error
		page, err := g.GetIngredients(ctx, location, f, skip+limit)
		if err != nil {
			return ingredients, nil
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
