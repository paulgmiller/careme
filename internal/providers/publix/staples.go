package publix

import (
	"context"
	"encoding/json"
	"fmt"
	"hash"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ingredients/cachekey"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	categoryVegetables = "837d6afb-a1d4-46a3-9015-b6db092ddb54"
	categoryFruit      = "21125cb8-4ba7-4038-a5c2-75899e205ce4"
	categoryBeef       = "163c7c04-5495-404e-81fc-34f71b241093"
	categoryVeal       = "206be70c-672c-4457-9e73-dc11d5412879"
	categoryChicken    = "6772da29-55bf-4051-83d5-104d73ae9a96"
	categoryLamb       = "e73c3cc5-be20-47f2-ae20-76dfa398ec06"
	categorySausage    = "20a53a52-81f3-4039-8758-0f703235a75b"
	categoryFish       = "eb84be44-d588-42b4-8e22-11016b4f5604"
	categoryScallops   = "c88b0e54-ef75-4408-9d3e-851f35c2b6d6"
	categoryPasta      = "e9f01489-6ce4-4c64-b5f5-2fe1e55da3c9"
	categoryRiceGrains = "b064da7d-7b01-426d-a122-450fba08f8a4"
	categoryWine       = "50eeb714-7cea-4a73-990a-43fa1a451bbd"

	defaultStapleTake = 48
	bigStapleTake     = 100
	produceStapleTake = 250

	wineTake = 200
	maxPage  = 100
)

var defaultStaplesSignature = lo.Must(json.Marshal(stapleCategories()))

type stapleCategory struct {
	Name  string
	ID    string
	Limit int
}

type savingsClient interface {
	StoreProductsSavings(ctx context.Context, opts storeProductsSavingsOptions) (*storeProductsSavingsResult, error)
}

type identityProvider struct{}

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	return cachekey.NewHash(locationID, date, string(defaultStaplesSignature))
}

type loadAbck func(context.Context) (string, error)

type staplesProvider struct {
	identityProvider
	client    savingsClient
	abckCache loadAbck
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(cfg config.PublixConfig, httpClient *http.Client) (staplesProvider, error) {
	abckCache, err := cache.EnsureCache(Container)
	if err != nil {
		return staplesProvider{}, fmt.Errorf("failed to create publix cache for abck token: %w", err)
	}

	return newStaplesProviderWithCache(newSearchClient(httpClient), func(ctx context.Context) (string, error) {
		return cookieFromCache(ctx, abckCache)
	}), nil
}

func cookieFromCache(ctx context.Context, c cache.Cache) (string, error) {
	abck, err := loadLatestAbck(ctx, c)
	if err != nil {
		return "", fmt.Errorf("load cached publix abck token: %w", err)
	}
	return abck.Cookie, nil
}

func newStaplesProviderWithCache(client savingsClient, loadAbck loadAbck) staplesProvider {
	return staplesProvider{
		client:    client,
		abckCache: loadAbck,
	}
}

func (p identityProvider) IsID(locationID string) bool {
	_, ok := parseLocationID(locationID)
	return ok
}

func (p staplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	storeID, err := storeIDFromLocation(locationID)
	if err != nil {
		return nil, err
	}
	if p.client == nil {
		return nil, fmt.Errorf("publix client is required")
	}
	abck, err := p.abckCache(ctx)
	if err != nil {
		return nil, err
	}

	return parallelism.Flatten(stapleCategories(), func(category stapleCategory) ([]ai.InputIngredient, error) {
		products, err := p.fetchCategoryProducts(ctx, storeID, abck, category)
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch publix category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(products, func(product storeProduct, _ int) ai.InputIngredient {
			return productToIngredient(product, category)
		})
		priceLineCount, originalPriceLineCount := countProductPriceLines(products)
		slog.InfoContext(
			ctx,
			"found publix staples for category",
			"count",
			len(ingredients),
			"priceLineCount",
			priceLineCount,
			"originalPriceLineCount",
			originalPriceLineCount,
			"category",
			category.Name,
			"location",
			locationID,
		)
		return ingredients, nil
	})
}

func (p staplesProvider) fetchCategoryProducts(ctx context.Context, storeID, abck string, category stapleCategory) ([]storeProduct, error) {
	limit := category.Limit
	products := make([]storeProduct, 0, limit)
	for skip := 0; len(products) < limit; {
		take := min(limit-len(products), maxPage)
		payload, err := p.client.StoreProductsSavings(ctx, storeProductsSavingsOptions{
			StoreNumber: storeID,
			CategoryID:  category.ID,
			Abck:        abck,
			Take:        take,
			Skip:        skip,
		})
		if err != nil {
			return nil, err
		}

		products = append(products, payload.StoreProducts...)
		if len(products) >= limit {
			break
		}
		if len(payload.StoreProducts) < take {
			break
		}
		skip += take
	}
	return products, nil
}

func (p staplesProvider) FetchWines(ctx context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	storeID, err := storeIDFromLocation(locationID)
	if err != nil {
		return nil, err
	}
	if p.client == nil {
		return nil, fmt.Errorf("publix client is required")
	}
	abck, err := p.abckCache(ctx)
	if err != nil {
		return nil, err
	}

	category := stapleCategory{Name: "wine", ID: categoryWine, Limit: wineTake}
	products, err := p.fetchCategoryProducts(ctx, storeID, abck, category)
	if err != nil {
		return nil, err
	}
	return lo.Map(products, func(product storeProduct, _ int) ai.InputIngredient {
		return productToIngredient(product, category)
	}), nil
}

func countProductPriceLines(products []storeProduct) (int, int) {
	var priceLineCount int
	var originalPriceLineCount int
	for _, product := range products {
		if strings.TrimSpace(stringValue(product.PriceLine)) != "" {
			priceLineCount++
		}
		if strings.TrimSpace(stringValue(product.OriginalPriceLine)) != "" {
			originalPriceLineCount++
		}
	}
	return priceLineCount, originalPriceLineCount
}

func stapleCategories() []stapleCategory {
	return []stapleCategory{
		{Name: "vegetables", ID: categoryVegetables, Limit: produceStapleTake},
		{Name: "fruit", ID: categoryFruit, Limit: produceStapleTake},
		{Name: "beef", ID: categoryBeef, Limit: bigStapleTake},
		{Name: "veal", ID: categoryVeal, Limit: defaultStapleTake},
		{Name: "chicken", ID: categoryChicken, Limit: bigStapleTake},
		{Name: "lamb", ID: categoryLamb, Limit: defaultStapleTake},
		{Name: "sausage", ID: categorySausage, Limit: defaultStapleTake},
		{Name: "fish", ID: categoryFish, Limit: bigStapleTake},
		{Name: "scallops", ID: categoryScallops, Limit: defaultStapleTake},
		{Name: "pasta", ID: categoryPasta, Limit: bigStapleTake},
		{Name: "rice and grains", ID: categoryRiceGrains, Limit: bigStapleTake},
	}
}

func storeIDFromLocation(locationID string) (string, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return "", fmt.Errorf("invalid publix location id %q", locationID)
	}
	return strings.TrimPrefix(normalized, locationIDPrefix), nil
}

func parseLocationID(locationID string) (string, bool) {
	locationID = strings.TrimSpace(locationID)
	if !strings.HasPrefix(locationID, locationIDPrefix) {
		return "", false
	}

	storeID := strings.TrimPrefix(locationID, locationIDPrefix)
	if storeID == "" {
		return "", false
	}
	return locationIDPrefix + storeID, true
}

func productToIngredient(product storeProduct, category stapleCategory) ai.InputIngredient {
	salePrice := priceFromLine(stringValue(product.PriceLine))
	regularPrice := priceFromLine(stringValue(product.OriginalPriceLine))
	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:    strconv.Itoa(product.ItemCode),
		Description:  product.Title,
		Size:         stringValue(product.SizeDescription),
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		AisleNumber:  category.Name,
		Categories:   []string{category.Name},
	})
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Publix price lines are display text, not structured pricing.
// This handles plain prices like "$3.49" and multi-buy strings like "2 for $5.00" or "2/$5.00".
var priceLinePattern = regexp.MustCompile(`(?i)(?:(\d+)\s*(?:for|/)\s*)?\$([0-9]+(?:\.[0-9]{1,2})?)`)

func priceFromLine(priceLine string) *float32 {
	matches := priceLinePattern.FindStringSubmatch(priceLine)
	if len(matches) == 0 {
		return nil
	}

	price, err := strconv.ParseFloat(matches[2], 32)
	if err != nil || price <= 0 {
		return nil
	}

	if matches[1] != "" {
		count, err := strconv.ParseFloat(matches[1], 32)
		if err != nil || count <= 0 {
			return nil
		}
		price = price / count
	}

	out := float32(price)
	return &out
}
