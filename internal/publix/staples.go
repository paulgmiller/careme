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
	CategoryBeef       = "163c7c04-5495-404e-81fc-34f71b241093"

	defaultStapleTake = 48
)

var defaultStaplesSignature = lo.Must(json.Marshal(StapleCategories()))

type StapleCategory struct {
	Name string `json:"name"`
	ID   string `json:"id"`
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
		client: NewClient(httpClient),
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
			Take:        defaultStapleTake,
			Skip:        0,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch publix category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(payload.StoreProducts, func(product StoreProduct, _ int) ai.InputIngredient {
			return productToIngredient(product, category)
		})
		slog.InfoContext(ctx, "found publix staples for category", "count", len(ingredients), "category", category.Name, "location", locationID)
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

func StapleCategories() []StapleCategory {
	return []StapleCategory{
		{Name: "vegetables", ID: CategoryVegetables},
		{Name: "beef", ID: CategoryBeef},
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
	if strings.EqualFold(input, "vegatables") {
		return StapleCategory{Name: "vegetables", ID: CategoryVegetables}, true
	}
	return StapleCategory{Name: input, ID: input}, true
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
	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:   strconv.Itoa(product.ItemCode),
		Description: product.Title,
		Size:        stringValue(product.SizeDescription),
		PriceSale:   salePrice,
		AisleNumber: category.Name,
		Categories:  []string{category.Name},
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
