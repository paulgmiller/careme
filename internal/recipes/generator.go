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
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo/mutable"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstruction string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	Ready(ctx context.Context) error
}

type Generator struct {
	config       *config.Config
	aiClient     aiClient
	krogerClient kroger.ClientWithResponsesInterface // probably need only subset
	cache        cache.Cache
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
	}, nil
}

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
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
		// This ended up giving me a "Preference update + replacements requested" recipe
		// instructions += saved.Title + "; " //is this enough or do we keep the exact one?
		shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

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

	//TODO this does not get saved in params and thus must be loaded from html
	// could update params after first generation or pregenerate before we save params.
	p.ConversationID = shoppingList.ConversationID
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
}

func (g *Generator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	return g.aiClient.AskQuestion(ctx, question, conversationID)
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

// GetIngredientsForFilters fetches and combines ingredients for the provided filters in parallel.
// Errors from individual filters are logged and skipped so other filters can still contribute results.
func (g *Generator) GetIngredientsForFilters(ctx context.Context, locationID string, filters ...filter) []kroger.Ingredient {
	var wg sync.WaitGroup
	var lock sync.Mutex //channel instead?
	var ingredients []kroger.Ingredient

	wg.Add(len(filters))
	for _, category := range filters {
		go func(category filter) {
			defer wg.Done()
			cingredients, err := g.GetIngredients(ctx, locationID, category, 0)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get ingredients", "category", category.Term, "location", locationID, "error", err)
				return
			}

			lock.Lock()
			ingredients = append(ingredients, cingredients...)
			lock.Unlock()

			slog.InfoContext(ctx, "Found ingredients for category", "count", len(cingredients), "category", category.Term, "location", locationID)
		}(category)
	}

	wg.Wait()
	return ingredients
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]kroger.Ingredient, error) {
	lochash := p.LocationHash()
	rio := IO(g.cache)

	if cachedIngredients, err := rio.IngredientsFromCache(ctx, lochash); err == nil {
		slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	ingredients := g.GetIngredientsForFilters(ctx, p.Location.ID, p.Staples...)

	mutable.Shuffle(ingredients)

	if err := rio.SaveIngredients(ctx, p.LocationHash(), ingredients); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return ingredients, nil
}

// move to krogrer client as everyone will be differnt here?
func (g *Generator) GetIngredients(ctx context.Context, location string, f filter, skip int) ([]kroger.Ingredient, error) {
	limit := 50
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	// brand := "empty" doesn't work have to check for nil
	// fulfillment := "ais" drmatically shortens?
	// wrapped this in a retry and it did nothng
	products, err := g.krogerClient.ProductSearchWithResponse(ctx, &kroger.ProductSearchParams{
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
				Categories:   product.Categories,
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
	// kroger limits us at 250 results.
	if len(*products.JSON200.Data) == limit && skip < 250 { // fence post error
		page, err := g.GetIngredients(ctx, location, f, skip+limit)
		if err != nil {
			slog.ErrorContext(ctx, "ending pagination after page fetch error", "error", err, "term", f.Term, "location", location, "skip", skip+limit)
			return ingredients, nil
		}
		ingredients = append(ingredients, page...)
	}

	return ingredients, nil
}

func (g *Generator) Ready(ctx context.Context) error {
	return g.aiClient.Ready(ctx)
}

// toStr returns the string value if non-nil, or "empty" otherwise.
func toStr(s *string) string {
	if s == nil {
		return "empty"
	}
	return *s
}
