package recipes

import (
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/wholefoods"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

type staplesProvider interface {
	FetchStaples(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error)
}

type staplesProviderFunc func(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error)

func (f staplesProviderFunc) FetchStaples(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error) {
	return f(ctx, location, staples)
}

type wholeFoodsCategoryClient interface {
	Category(ctx context.Context, queryterm, store string) (*wholefoods.CategoryResponse, error)
}

type routingStaplesProvider struct {
	kroger     staplesProvider
	wholeFoods staplesProvider
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error) {
	if location == nil {
		return nil, fmt.Errorf("location is required")
	}

	switch {
	case strings.HasPrefix(location.ID, wholefoods.LocationIDPrefix):
		if p.wholeFoods == nil {
			return nil, fmt.Errorf("whole foods staples provider not configured")
		}
		return p.wholeFoods.FetchStaples(ctx, location, staples)
	case strings.HasPrefix(location.ID, "walmart_"):
		return nil, fmt.Errorf("staples provider does not support location %q", location.ID)
	default:
		if p.kroger == nil {
			return nil, fmt.Errorf("kroger staples provider not configured")
		}
		return p.kroger.FetchStaples(ctx, location, staples)
	}
}

type krogerStaplesProvider struct {
	getIngredients func(ctx context.Context, location string, f filter, skip int) ([]kroger.Ingredient, error)
}

func (p krogerStaplesProvider) FetchStaples(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error) {
	ingredients, err := asParallel(staples, func(category filter) ([]kroger.Ingredient, error) {
		found, err := p.getIngredients(ctx, location.ID, category, 0)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get kroger ingredients", "category", category.Term, "location", location.ID, "error", err)
			return nil, err
		}
		slog.InfoContext(ctx, "Found Kroger ingredients for category", "count", len(found), "category", category.Term, "location", location.ID)
		return found, nil
	})
	if err != nil {
		return nil, err
	}
	return ingredients, nil
}

type wholeFoodsStaplesProvider struct {
	client wholeFoodsCategoryClient
}

func (p wholeFoodsStaplesProvider) FetchStaples(ctx context.Context, location *locations.Location, staples []filter) ([]kroger.Ingredient, error) {
	if location == nil {
		return nil, fmt.Errorf("location is required")
	}
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	storeID := strings.TrimPrefix(location.ID, wholefoods.LocationIDPrefix)
	if storeID == location.ID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", location.ID)
	}

	ingredients, err := asParallel(staples, func(category filter) ([]kroger.Ingredient, error) {
		resp, err := p.client.Category(ctx, category.Term, storeID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get whole foods products", "category", category.Term, "location", location.ID, "error", err)
			return nil, err
		}

		found := make([]kroger.Ingredient, 0, len(resp.Results))
		for _, product := range resp.Results {
			found = append(found, wholeFoodsProductToIngredient(product))
		}
		slog.InfoContext(ctx, "Found Whole Foods ingredients for category", "count", len(found), "category", category.Term, "location", location.ID)
		return found, nil
	})
	if err != nil {
		return nil, err
	}
	return ingredients, nil
}

func wholeFoodsProductToIngredient(product wholefoods.Product) kroger.Ingredient {
	var regularPrice *float32
	if product.RegularPrice > 0 {
		price := float32(product.RegularPrice)
		regularPrice = &price
	}

	var salePrice *float32
	if product.SalePrice > 0 {
		price := float32(product.SalePrice)
		salePrice = &price
	}

	var size *string
	sizeText := strings.TrimSpace(strings.Join(compactStrings(product.UOM), " "))
	if sizeText != "" {
		size = &sizeText
	}

	productID := strconv.Itoa(product.Store) + ":" + product.Slug
	productName := strings.TrimSpace(product.Name)
	brand := strings.TrimSpace(product.Brand)
	categories := compactStrings(localCategory(product))

	return kroger.Ingredient{
		ProductId:    stringPtr(productID),
		Brand:        stringPtr(brand),
		Description:  stringPtr(productName),
		Size:         size,
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		Categories:   slicePtr(categories),
	}
}

func localCategory(product wholefoods.Product) string {
	if product.IsLocal {
		return "Local"
	}
	return ""
}

func compactStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func slicePtr(values []string) *[]string {
	if len(values) == 0 {
		return nil
	}
	return &values
}

func krogerError(statusCode int, payload any) error {
	output, _ := json.Marshal(payload)
	return fmt.Errorf("got %d code from kroger : %s", statusCode, string(output))
}

func requireKrogerSuccess(statusCode int, payload any) error {
	if statusCode == http.StatusOK {
		return nil
	}
	return krogerError(statusCode, payload)
}
