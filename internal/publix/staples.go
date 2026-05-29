package publix

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	CategoryVegetables = "837d6afb-a1d4-46a3-9015-b6db092ddb54"
	CategoryFruit      = "21125cb8-4ba7-4038-a5c2-75899e205ce4"
	CategoryBeef       = "163c7c04-5495-404e-81fc-34f71b241093"
	CategoryVeal       = "206be70c-672c-4457-9e73-dc11d5412879"
	CategoryChicken    = "6772da29-55bf-4051-83d5-104d73ae9a96"
	CategoryLamb       = "e73c3cc5-be20-47f2-ae20-76dfa398ec06"
	CategorySausage    = "20a53a52-81f3-4039-8758-0f703235a75b"
	CategoryFish       = "eb84be44-d588-42b4-8e22-11016b4f5604"
	CategoryScallops   = "c88b0e54-ef75-4408-9d3e-851f35c2b6d6"
	CategoryPasta      = "e9f01489-6ce4-4c64-b5f5-2fe1e55da3c9"
	CategoryRiceGrains = "b064da7d-7b01-426d-a122-450fba08f8a4"

	defaultStapleTake = 48
	bigStapleTake     = 100
)

var defaultStaplesSignature = lo.Must(json.Marshal(StapleCategories()))

type StapleCategory struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Take int    `json:"take,omitempty"`
}

type savingsClient interface {
	StoreProductsSavings(ctx context.Context, opts StoreProductsSavingsOptions) (*StoreProductsSavingsResult, error)
}

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
	client savingsClient
	abck   string
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(cfg config.PublixConfig, httpClient *http.Client) StaplesProvider {
	return StaplesProvider{
		client: NewSearchClient(httpClient),
		abck:   cfg.Abck,
	}
}

func newStaplesProvider(client savingsClient, abck string) StaplesProvider {
	return StaplesProvider{
		client: client,
		abck:   abck,
	}
}

func (p identityProvider) Signature() string {
	return string(defaultStaplesSignature)
}

func (p identityProvider) IsID(locationID string) bool {
	_, ok := parseLocationID(locationID)
	return ok
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	storeID, err := storeIDFromLocation(locationID)
	if err != nil {
		return nil, err
	}
	if p.client == nil {
		return nil, fmt.Errorf("publix client is required")
	}
	if strings.TrimSpace(p.abck) == "" {
		return nil, fmt.Errorf("publix abck token is required")
	}

	return parallelism.Flatten(StapleCategories(), func(category StapleCategory) ([]ai.InputIngredient, error) {
		payload, err := p.client.StoreProductsSavings(ctx, StoreProductsSavingsOptions{
			StoreNumber: storeID,
			CategoryID:  category.ID,
			Abck:        p.abck,
			Take:        category.Take,
			Skip:        0,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch publix category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(payload.StoreProducts, func(product StoreProduct, _ int) ai.InputIngredient {
			return productToIngredient(product, category)
		})
		priceLineCount, originalPriceLineCount := countProductPriceLines(payload.StoreProducts)
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

func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, categoryID string, skip int) ([]ai.InputIngredient, error) {
	storeID, err := storeIDFromLocation(locationID)
	if err != nil {
		return nil, err
	}
	if p.client == nil {
		return nil, fmt.Errorf("publix client is required")
	}
	if strings.TrimSpace(p.abck) == "" {
		return nil, fmt.Errorf("publix abck token is required")
	}
	category, ok := stapleCategoryFromInput(categoryID)
	if !ok {
		return nil, fmt.Errorf("publix category id is required")
	}

	payload, err := p.client.StoreProductsSavings(ctx, StoreProductsSavingsOptions{
		StoreNumber: storeID,
		CategoryID:  category.ID,
		Abck:        p.abck,
		Take:        defaultStapleTake,
		Skip:        skip,
	})
	if err != nil {
		return nil, err
	}

	return lo.Map(payload.StoreProducts, func(product StoreProduct, _ int) ai.InputIngredient {
		return productToIngredient(product, category)
	}), nil
}

func countProductPriceLines(products []StoreProduct) (int, int) {
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

func StapleCategories() []StapleCategory {
	return []StapleCategory{
		// get capped at 100 need to paginate vegtables and fruit
		{Name: "vegetables", ID: CategoryVegetables, Take: bigStapleTake},
		{Name: "fruit", ID: CategoryFruit, Take: bigStapleTake},
		{Name: "beef", ID: CategoryBeef, Take: bigStapleTake},
		{Name: "veal", ID: CategoryVeal, Take: defaultStapleTake},
		{Name: "chicken", ID: CategoryChicken, Take: bigStapleTake},
		{Name: "lamb", ID: CategoryLamb, Take: defaultStapleTake},
		{Name: "sausage", ID: CategorySausage, Take: defaultStapleTake},
		{Name: "fish", ID: CategoryFish, Take: bigStapleTake},
		{Name: "scallops", ID: CategoryScallops, Take: defaultStapleTake},
		{Name: "pasta", ID: CategoryPasta, Take: bigStapleTake},
		{Name: "rice and grains", ID: CategoryRiceGrains, Take: bigStapleTake},
	}
}

func stapleCategoryFromInput(input string) (StapleCategory, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return StapleCategory{}, false
	}
	for _, category := range StapleCategories() {
		if input == category.ID || strings.EqualFold(input, category.Name) {
			return category, true
		}
	}
	return StapleCategory{}, false
}

func storeIDFromLocation(locationID string) (string, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return "", fmt.Errorf("invalid publix location id %q", locationID)
	}
	return strings.TrimPrefix(normalized, LocationIDPrefix), nil
}

func parseLocationID(locationID string) (string, bool) {
	locationID = strings.TrimSpace(locationID)
	if !strings.HasPrefix(locationID, LocationIDPrefix) {
		return "", false
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == "" {
		return "", false
	}
	return LocationIDPrefix + storeID, true
}

func productToIngredient(product StoreProduct, category StapleCategory) ai.InputIngredient {
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
