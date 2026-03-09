package wholefoods

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"careme/internal/kroger"

	lop "github.com/samber/lo/parallel"
)

const DefaultStaplesSignature = "wholefoods-staples-v1"

type CategoryClient interface {
	Category(ctx context.Context, queryterm, store string) (*CategoryResponse, error)
}

type StaplesProvider struct {
	client CategoryClient
}

func NewStaplesProvider(client CategoryClient) StaplesProvider {
	return StaplesProvider{client: client}
}

func (p StaplesProvider) Signature() string {
	return DefaultStaplesSignature
}

func (p StaplesProvider) IsID(locationID string) bool {
	_, ok := parseLocationID(locationID)
	return ok
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == locationID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", locationID)
	}

	return flattenParallel(defaultStaples(), func(category string) ([]kroger.Ingredient, error) {
		resp, err := p.client.Category(ctx, category, storeID)
		if err != nil {
			return nil, err
		}

		found := make([]kroger.Ingredient, 0, len(resp.Results))
		for _, product := range resp.Results {
			found = append(found, productToIngredient(product))
		}
		return found, nil
	})
}

func defaultStaples() []string {
	return []string{
		"vegetables",
		"fruit",
		"beef",
		"chicken",
		"fish",
		"pork",
		"shellfish",
		"lamb",
	}
}

func productToIngredient(product Product) kroger.Ingredient {
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

func localCategory(product Product) string {
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

func flattenParallel[T any, T2 any](items []T, fn func(T) ([]T2, error)) ([]T2, error) {
	if len(items) == 0 {
		return []T2{}, nil
	}

	type result struct {
		values []T2
		err    error
	}

	mapped := lop.Map(items, func(item T, _ int) result {
		values, err := fn(item)
		return result{values: values, err: err}
	})

	merged := make([]T2, 0)
	errs := make([]error, 0)
	for _, r := range mapped {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		merged = append(merged, r.values...)
	}

	return merged, errors.Join(errs...)
}
