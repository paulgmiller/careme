package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	Ready(ctx context.Context) error
}

type ingredientio interface {
	SaveIngredients(ctx context.Context, hash string, ingredients []kroger.Ingredient) error
	IngredientsFromCache(ctx context.Context, hash string) ([]kroger.Ingredient, error)
}

type Generator struct {
	config       *config.Config
	aiClient     aiClient
	krogerClient kroger.ClientWithResponsesInterface // probably need only subset
	io           ingredientio
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
		io:           IO(cache),
		config:       cfg,
		aiClient:     ai.NewClient(cfg.AI.APIKey, "TODOMODEL"),
		krogerClient: client,
	}, nil
}

func (g *Generator) PickAWine(ctx context.Context, conversationID string, location string, recipe ai.Recipe, date time.Time) (string, error) {
	styles := make([]string, 0, len(recipe.WineStyles)+1)
	for _, style := range recipe.WineStyles {
		style = strings.TrimSpace(style)
		if style != "" {
			styles = append(styles, style)
		}
	}
	if len(styles) == 0 {
		return "", fmt.Errorf("no wine styles available for recipe %q", recipe.Title)
	}

	cacheDate := date
	wines, err := asParallel(styles, func(style string) ([]kroger.Ingredient, error) {
		cacheKey := wineIngredientsCacheKey(style, location, cacheDate)
		winesOfStyle, err := g.io.IngredientsFromCache(ctx, cacheKey)
		if err == nil {
			slog.InfoContext(ctx, "Serving cached wines for style", "style", style, "location", location, "date", cacheDate.Format("2006-01-02"), "count", len(winesOfStyle))
			return winesOfStyle, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "Failed to read cached wines for style", "style", style, "location", location, "date", cacheDate.Format("2006-01-02"), "error", err)
		}

		slog.InfoContext(ctx, "Picking wine for style", "style", style)
		winesOfStyle, err = g.GetIngredients(ctx, location, Filter(style, []string{"*"}, false), 0)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get ingredients for wine style", "style", style, "error", err)
			return nil, fmt.Errorf("failed to get ingredients for style %q: %w", style, err)
		}

		if err := g.io.SaveIngredients(ctx, cacheKey, winesOfStyle); err != nil {
			slog.ErrorContext(ctx, "Failed to cache wines for style", "style", style, "location", location, "date", cacheDate.Format("2006-01-02"), "error", err)
		}
		return winesOfStyle, nil
	})
	if err != nil {
		return "", err
	}

	if len(wines) == 0 {
		return "no wines found ", nil
	}
	wines = lo.UniqBy(wines, func(i kroger.Ingredient) string { return strings.ToLower(toStr(i.Description)) })

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Pick a wine that would go well with %q. Here are %d wines in TSV format.\n", recipe.Title, len(wines)))
	err = kroger.ToTSV(wines, &sb)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert wines to TSV", "error", err)
		return "", err
	}
	return g.aiClient.AskQuestion(ctx, sb.String(), conversationID)
}

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
	start := time.Now()

	if p.ConversationID != "" && (p.Instructions != "" || len(p.Saved) > 0 || len(p.Dismissed) > 0) {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "conversation_id", p.ConversationID)
		// these should both always be true. Warn if not because its a caching bug?
		instructions := []string{p.Instructions}
		for _, dismissed := range p.Dismissed {
			instructions = append(instructions, "Passed on "+dismissed.Title)
		}

		shoppingList, err := g.aiClient.Regenerate(ctx, instructions, p.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		// want to add saved to insructions but only once. TODO
		// Include saved recipes in the shopping list
		shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return shoppingList, nil
	}
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}

	instructions := []string{p.Directive, p.Instructions}

	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
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

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]kroger.Ingredient, error) {
	lochash := p.LocationHash()
	var ingredients []kroger.Ingredient

	if cachedIngredients, err := g.io.IngredientsFromCache(ctx, lochash); err == nil {
		slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	ingredients, err := asParallel(p.Staples, func(category filter) ([]kroger.Ingredient, error) {
		cingredients, err := g.GetIngredients(ctx, p.Location.ID, category, 0)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get ingredients", "category", category.Term, "location", p.Location.ID, "error", err)
			return nil, err
		}
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(cingredients), "category", category.Term, "location", p.Location.ID, "runningtotal", len(ingredients))
		return cingredients, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples: %w", err)
	}
	ingredients = lo.UniqBy(ingredients, func(i kroger.Ingredient) string { return toStr(i.Description) })
	mutable.Shuffle(ingredients)

	if err := g.io.SaveIngredients(ctx, p.LocationHash(), ingredients); err != nil {
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

			var aisle *string
			if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
				aisle = (*product.AisleLocations)[0].Number
			}

			// does just giving the model json work better here?
			ingredient := kroger.Ingredient{
				ProductId:    product.ProductId, //chat gpt act
				Brand:        product.Brand,
				Description:  product.Description,
				Size:         item.Size,
				PriceRegular: item.Price.Regular,
				PriceSale:    item.Price.Promo,
				Categories:   product.Categories,
				AisleNumber:  aisle,

				/*"taxonomies": [
				{
				"department": {},
				"commodity": {},
				"subCommodity": {}
				}
				],*/
				//Taxonomy:  product.,
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
	// kroger limites us to 250
	if len(*products.JSON200.Data) == limit && skip < 250 { // fence post error
		page, err := g.GetIngredients(ctx, location, f, skip+limit)
		if err != nil {
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

func wineIngredientsCacheKey(style, location string, date time.Time) string {
	normalizedStyle := strings.ToLower(strings.TrimSpace(style))
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, location))
	lo.Must(io.WriteString(fnv, date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, normalizedStyle))
	return "wines/" + base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}
