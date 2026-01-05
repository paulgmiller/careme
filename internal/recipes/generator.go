package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstruction string, conversationID string) (*ai.ShoppingList, error)
}

type Generator struct {
	config       *config.Config
	aiClient     aiClient
	krogerClient kroger.ClientWithResponsesInterface // probably need only subset
	cache        cache.Cache
	inflight     cache.Cache
}

func NewGenerator(cfg *config.Config, cache cache.Cache) (generator, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	client, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Generator{
		cache:        cache, // should this also pull from config?
		config:       cfg,
		aiClient:     ai.NewClient(cfg.AI.APIKey, "TODOMODEL"),
		krogerClient: client,
		inflight:     cache, // separate?
	}, nil
}

// TODO move this into its own struct.
func (g *Generator) isGenerating(ctx context.Context, hash string) error {
	setInFlight := func() error {
		return g.inflight.Set(ctx, "inflight/"+hash, time.Now().Format(time.RFC3339Nano))
	}

	startblob, err := g.inflight.Get(ctx, "inflight/"+hash)
	if err != nil {
		if err != cache.ErrNotFound {
			//TODO retry
			return err
		}
		return setInFlight()
	}
	startbuf, err := io.ReadAll(startblob)
	if err != nil {
		//TODO retry
		slog.ErrorContext(ctx, "failed to read inflight start time", "hash", hash, "error", err)
		return err
	}

	start, err := time.Parse(time.RFC3339Nano, string(startbuf))
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse inflight start time", "hash", hash, "error", err)
		return setInFlight() //just set it its not like its going tstart parsing
	}

	if time.Since(start) < 10*time.Minute {
		slog.InfoContext(ctx, "generation already in progress", "hash", hash, "since", time.Since(start))
		return InProgress
	}
	return setInFlight()

}

var InProgress error = errors.New("generation in progress")

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
	err := g.isGenerating(ctx, hash)
	if err != nil {
		return nil, err
	}
	start := time.Now()

	if p.ConversationID != "" && (p.Instructions != "" || len(p.Saved) > 0 || len(p.Dismissed) > 0) {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "conversation_id", p.ConversationID)
		// these should both always be true. Warn if not because its a caching bug?
		instructions := p.Instructions
		var dismissedTitles []string
		for _, dismissed := range p.Dismissed {
			dismissedTitles = append(dismissedTitles, dismissed.Title)
		}
		if len(p.Dismissed) > 0 {
			instructions += " Did not like " + strings.Join(dismissedTitles, "; ")
		}
		// TODO pipe through dismissed and saved so we dont mess with instructions. Also format dismissed titles with toon?
		shoppingList, err := g.aiClient.Regenerate(ctx, instructions, p.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		// Include saved recipes in the shopping list

		/*if len(p.Saved) > 0 {
			instructions += " Enjoyed and saved :"
		}*/
		for _, saved := range p.Saved {
			saved.Saved = true
			// This ended up giving me a "Preference update + replacements requested" recipe
			// instructions += saved.Title + "; " //is this enough or do we keep the exact one?
			shoppingList.Recipes = append(shoppingList.Recipes, saved)
		}

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return shoppingList, nil
	}
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}
	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, p.Instructions, p.Date, p.LastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	// should never happen? How do you get save on first generte?
	// shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

	p.ConversationID = shoppingList.ConversationID
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
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

	if ingredientblob, err := g.cache.Get(ctx, lochash); err == nil {
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
	if err := g.cache.Set(ctx, p.LocationHash(), string(allingredientsJSON)); err != nil {
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
	// brand := "empty" doesn't work have to check for nil
	// fulfillment := "ais" drmatically shortens?
	// wrapped this in a retry and it did nothng
	products, err := g.krogerClient.ProductSearchWithResponse(context.TODO(), &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &f.Term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
		// FilterBrand:      &brand,
		// FilterFulfillment: &fulfillment,
	})
	if err != nil {
		return nil, fmt.Errorf("kroger product search request failed: %w", err)
	}
	if products.StatusCode() != http.StatusOK {
		output, _ := json.Marshal(products.JSON500) // handle other errors?
		return nil, fmt.Errorf("got %d code from kroger : %s", products.StatusCode(), string(output))
	}

	var ingredients []kroger.Ingredient

	for _, product := range *products.JSON200.Data {
		wildcard := len(f.Brands) > 0 && f.Brands[0] == "*"

		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) && !wildcard {
			continue
		}
		// end up with a bunch of frozen chicken with out this.
		if slices.Contains(*product.Categories, "Frozen") && !f.Frozen {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				// todo what does this mean?
				continue
			}

			// does just giving the model json work better here?
			ingredient := kroger.Ingredient{
				Brand:        product.Brand,
				Description:  product.Description,
				Size:         item.Size,
				PriceRegular: item.Price.Regular,
				PriceSale:    item.Price.Promo,
				// CountryOrigin: product.CountryOrigin,
				// AisleNumber:   product.AisleLocations[0].Number,
				// Favorite: item.Favorite,
				// InventoryStockLevel: item.InventoryStockLevel),
			}

			/*if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
				ingredient.AisleNumber = (*product.AisleLocations)[0].Number
			}*/

			ingredients = append(ingredients, ingredient)
			// strings.Join(*product.Categories, ", "),

		}
	}

	// Debug level?
	// slog.InfoContext(ctx, "got", "ingredients", len(ingredients), "products", len(*products.JSON200.Data), "term", f.Term, "brands", f.Brands, "location", location, "skip", skip)

	// recursion is pretty dumb pagination
	// 500's seem gone.
	if len(*products.JSON200.Data) == limit && skip+limit < 100 { // fence post error
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
