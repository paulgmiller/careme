package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ingredients/cachekey"
	"careme/internal/parallelism"
	"careme/internal/providers/kroger/products"

	"github.com/samber/lo"
	"golang.org/x/sync/singleflight"
)

var defaultStaplesSignature = string(lo.Must(json.Marshal(defaultStaples())))

type staplesFilter struct {
	Term   string `json:"term,omitempty"`
	Frozen bool   `json:"frozen,omitempty"`
}

type identityProvider struct{}

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	return cachekey.NewHash(locationID, date, "item-upc-v1/"+defaultStaplesSignature)
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (p identityProvider) IsID(locationID string) bool {
	if locationID == "" {
		return false
	}
	for i := 0; i < len(locationID); i++ {
		if locationID[i] < '0' || locationID[i] > '9' {
			return false
		}
	}
	return true
}

// internal?
type staplesProvider struct {
	identityProvider
	client         *products.ClientWithResponses
	cache          cache.Cache
	catalogFetches *singleflight.Group
}

func NewStaplesProvider(cfg *config.Config, httpClient *http.Client) (*staplesProvider, error) {
	client, err := newProductsClientFromConfig(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	cacheStore, err := cache.EnsureCache("recipes")
	if err != nil {
		return nil, fmt.Errorf("create Kroger wine catalog cache: %w", err)
	}
	return newStaplesProviderWithCache(client, cacheStore), nil
}

func newStaplesProviderWithCache(client *products.ClientWithResponses, c cache.Cache) *staplesProvider {
	return &staplesProvider{client: client, cache: c, catalogFetches: &singleflight.Group{}}
}

func (p staplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	return parallelism.Flatten(defaultStaples(), func(category staplesFilter) ([]ai.InputIngredient, error) {
		ingredients, err := searchIngredients(ctx, p.client, locationID, category.Term, category.Frozen)
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch category", "category", category.Term, "location", locationID, "error", err)
			return nil, err
		}
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(ingredients), "category", category.Term, "location", locationID)
		return lo.Map(ingredients, inputIngredientFromKrogerIngredient), nil
	})
}

// FetchPantry returns the separate pantry catalog used for meal-plan retrieval.
func (p staplesProvider) FetchPantry(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	return parallelism.Flatten(pantryFilters(), func(category staplesFilter) ([]ai.InputIngredient, error) {
		ingredients, err := searchIngredients(ctx, p.client, locationID, category.Term, false)
		if err != nil {
			return nil, err
		}
		return lo.Map(ingredients, func(ingredient ingredient, index int) ai.InputIngredient {
			input := inputIngredientFromKrogerIngredient(ingredient, index)
			if !slices.Contains(input.Categories, category.Term) {
				// dubious becsuse we don't do this for staples but if do embeddign based rag later its
				// somwhat usefult to pull from each categotry
				input.Categories = append(input.Categories, category.Term)
			}
			return input
		}), nil
	})
}

// PantryCategories returns the search terms used to source pantry products.
func PantryCategories() []string {
	return lo.Map(pantryFilters(), func(filter staplesFilter, _ int) string { return filter.Term })
}

func (p staplesProvider) FetchWines(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error) {
	wines, err := parallelism.Flatten([]func() ([]ai.InputIngredient, error){
		func() ([]ai.InputIngredient, error) { return p.searchWineStyles(ctx, locationID, styles) },
		func() ([]ai.InputIngredient, error) { return p.fetchWineCatalog(ctx, locationID, time.Now()) },
	}, func(fetch func() ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) { return fetch() })
	if err != nil {
		return nil, err
	}
	return wines, nil
}

func wineCatalogCacheKey(locationID string, date time.Time) string {
	return "kroger/wines/red-white-v1/" + locationID + "/" + date.UTC().Format("2006-01-02")
}

// The API returns current inventory, so the broad catalog uses the lookup day,
// independently of a recipe's shopping date or requested styles.
func (p staplesProvider) fetchWineCatalog(ctx context.Context, locationID string, date time.Time) ([]ai.InputIngredient, error) {
	key := wineCatalogCacheKey(locationID, date)
	result, err, _ := p.catalogFetches.Do(key, func() (any, error) {
		return p.loadWineCatalog(ctx, locationID, key)
	})
	if err != nil {
		return nil, err
	}
	return result.([]ai.InputIngredient), nil
}

func (p staplesProvider) loadWineCatalog(ctx context.Context, locationID, key string) ([]ai.InputIngredient, error) {
	reader, err := p.cache.Get(ctx, key)
	if err == nil {
		defer func() { _ = reader.Close() }()
		var wines []ai.InputIngredient
		if err := json.NewDecoder(reader).Decode(&wines); err != nil {
			return nil, fmt.Errorf("decode Kroger wine catalog %s: %w", key, err)
		}
		return wines, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("read Kroger wine catalog %s: %w", key, err)
	}
	wines, err := p.searchWineStyles(ctx, locationID, []string{"red wine", "white wine"})
	if err != nil {
		return nil, fmt.Errorf("fetch Kroger wine catalog for %s: %w", locationID, err)
	}
	body, err := json.Marshal(wines)
	if err != nil {
		return nil, fmt.Errorf("encode Kroger wine catalog for %s: %w", locationID, err)
	}
	if err := p.cache.Put(ctx, key, string(body), cache.Unconditional()); err != nil {
		return nil, fmt.Errorf("cache Kroger wine catalog for %s: %w", locationID, err)
	}
	return wines, nil
}

func (p staplesProvider) searchWineStyles(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error) {
	return parallelism.Flatten(styles, func(style string) ([]ai.InputIngredient, error) {
		ingredients, err := searchIngredients(ctx, p.client, locationID, style, false)
		if err != nil {
			return nil, err
		}
		return lo.Map(ingredients, inputIngredientFromKrogerIngredient), nil
	})
}

var availableInStore = products.Ais

func searchIngredients(ctx context.Context, client *products.ClientWithResponses, locationID, term string, frozen bool) ([]ingredient, error) {
	rows, err := (staplesProvider{client: client}).SearchProducts(ctx, locationID, term)
	if err != nil {
		return nil, err
	}

	var ingredients []ingredient

	for _, product := range rows {
		if !frozen && isFrozenProduct(product) {
			continue
		}
		// TODO(#1051): Revisit size-based selection as an alternative to inventory ranking.
		var selected *products.ProductsProductItemModel
		for _, item := range *product.Items {
			if item.Price == nil {
				continue
			}
			if item.Inventory != nil && item.Inventory.StockLevel != nil && *item.Inventory.StockLevel == products.TEMPORARILYOUTOFSTOCK {
				continue
			}
			if selected == nil || itemStockRank(item) > itemStockRank(*selected) {
				selected = &item
			}
		}
		if selected == nil {
			continue
		}
		var aisle *string
		if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
			aisle = (*product.AisleLocations)[0].Number
		}
		ingredients = append(ingredients, ingredient{
			ProductId:    product.ProductId,
			ItemId:       selected.ItemId,
			Brand:        product.Brand,
			Description:  product.Description,
			Size:         selected.Size,
			PriceRegular: selected.Price.Regular,
			PriceSale:    selected.Price.Promo,
			Categories:   product.Categories,
			AisleNumber:  aisle,
		})

		//DO we care about these?
		/*"taxonomies": [
		{
		"department": {},
		"commodity": {},
		"subCommodity": {}
		}
		],*/
		//Taxonomy:  product.,
		// CountryOrigin: product.CountryOrigin,
		// Favorite: item.Favorite,
	}

	return ingredients, nil
}

// SearchProducts returns in-store API products with every item variant intact.
// It shares pagination with the ingredient command and never filters brands.
func (p staplesProvider) SearchProducts(ctx context.Context, locationID, term string) ([]products.ProductsProductModel, error) {
	const limit = 50
	var matches []products.ProductsProductModel
	// Kroger allows page starts through 250 with 50 products per request.
	for start := 0; start <= 250; start += limit {
		response, err := p.client.ProductGetWithResponse(ctx, &products.ProductGetParams{
			FilterLocationId: &locationID, FilterTerm: &term,
			FilterLimit: new(limit), FilterStart: &start, FilterFulfillment: &availableInStore,
		})
		if err != nil {
			return nil, fmt.Errorf("kroger product search %q at offset %d: %w", term, start, err)
		}
		if err := requireSuccess(response.StatusCode(), productSearchErrorPayload(response)); err != nil {
			return nil, fmt.Errorf("kroger product search %q at offset %d: %w", term, start, err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, fmt.Errorf("kroger product search %q at offset %d: missing product data", term, start)
		}
		page := *response.JSON200.Data
		matches = append(matches, page...)
		if len(page) < limit {
			break
		}
	}
	return matches, nil
}

// isFrozenProduct also checks descriptions because Kroger omits the Frozen
// category on some frozen fillets. Previously frozen counter items are thawed.
func isFrozenProduct(product products.ProductsProductModel) bool {
	if slices.Contains(*product.Categories, "Frozen") {
		return true
	}
	words := strings.FieldsFunc(strings.ToLower(toStr(product.Description)), func(r rune) bool { return !unicode.IsLetter(r) })
	for i, word := range words {
		if word == "frozen" && (i == 0 || (words[i-1] != "previously" && words[i-1] != "never")) {
			return true
		}
	}
	return false
}

// itemStockRank prefers HIGH, then LOW, then unknown stock. Equal ranks retain API order.
func itemStockRank(item products.ProductsProductItemModel) int {
	if item.Inventory == nil || item.Inventory.StockLevel == nil {
		return 0
	}
	switch *item.Inventory.StockLevel {
	case products.HIGH:
		return 2
	case products.LOW:
		return 1
	default:
		return 0
	}
}

func inputIngredientFromKrogerIngredient(ingredient ingredient, _ int) ai.InputIngredient {
	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:    strings.TrimSpace(toStr(ingredient.ProductId)),
		VariantID:    strings.TrimSpace(toStr(ingredient.ItemId)),
		AisleNumber:  strings.TrimSpace(toStr(ingredient.AisleNumber)),
		Brand:        strings.TrimSpace(toStr(ingredient.Brand)),
		Description:  strings.TrimSpace(toStr(ingredient.Description)),
		Size:         strings.TrimSpace(toStr(ingredient.Size)),
		PriceRegular: clonePrice(ingredient.PriceRegular),
		PriceSale:    clonePrice(ingredient.PriceSale),
		Categories:   categoriesFromPtr(ingredient.Categories),
	})
}

func categoriesFromPtr(ptr *[]string) []string {
	if ptr == nil {
		return nil
	}
	return append([]string(nil), (*ptr)...)
}

func clonePrice(price *float32) *float32 {
	if price == nil {
		return nil
	}
	value := *price
	return &value
}

func defaultStaples() []staplesFilter {
	return append(produceFilters(), []staplesFilter{
		{
			Term: "beef",
		},
		{
			Term: "chicken",
		},
		{
			Term: "turkey",
		},
		{
			Term: "sausage",
		},
		{
			Term: "fish",
		},
		{
			Term: "pork",
		},
		{
			Term:   "shellfish",
			Frozen: true,
		},
		{
			Term: "lamb",
		},
		{
			Term: "grains",
		},
		{
			Term: "pasta",
		},
		{Term: "Dairy & Eggs"},
	}...)
}

func pantryFilters() []staplesFilter {
	return []staplesFilter{
		{Term: "spices"},
		{Term: "dairy"},
		{Term: "international"},
		// seasoning? herbs?
	}
}

func produceFilters() []staplesFilter {
	return []staplesFilter{
		{
			Term: "fresh vegetables",
		},
		{
			Term: "fresh produce",
		},
	}
}

func krogerError(statusCode int, payload any) error {
	output, _ := json.Marshal(payload)
	return fmt.Errorf("got %d code from kroger : %s", statusCode, string(output))
}

func requireSuccess(statusCode int, payload any) error {
	if statusCode == http.StatusOK {
		return nil
	}
	return krogerError(statusCode, payload)
}

func productSearchErrorPayload(resp *products.ProductGetResponse) any {
	if resp == nil {
		return nil
	}
	if len(resp.Body) != 0 {
		return json.RawMessage(resp.Body)
	}
	if resp.JSON400 != nil {
		return resp.JSON400
	}
	if resp.JSON401 != nil {
		return resp.JSON401
	}
	if resp.JSON500 != nil {
		return resp.JSON500
	}
	return nil
}

func mustJSONSignature(value any) string {
	signature, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("marshal staples signature: %w", err))
	}
	return string(signature)
}
