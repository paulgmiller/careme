package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	lop "github.com/samber/lo/parallel"
)

const DefaultStaplesSignature = "kroger-staples-v1"

type StaplesFilter struct {
	Term   string
	Brands []string
	Frozen bool
}

type StaplesProvider struct {
	client ClientWithResponsesInterface
}

type StoreIdentityProvider struct{}

func NewStaplesProvider(client ClientWithResponsesInterface) StaplesProvider {
	return StaplesProvider{client: client}
}

func NewStoreIdentityProvider() StoreIdentityProvider {
	return StoreIdentityProvider{}
}

func (p StaplesProvider) Signature() string {
	return NewStoreIdentityProvider().Signature()
}

func (p StaplesProvider) IsID(locationID string) bool {
	return NewStoreIdentityProvider().IsID(locationID)
}

func (p StoreIdentityProvider) Signature() string {
	return DefaultStaplesSignature
}

func (p StoreIdentityProvider) IsID(locationID string) bool {
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

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]Ingredient, error) {
	return flattenParallel(defaultStaples(), func(category StaplesFilter) ([]Ingredient, error) {
		return SearchIngredients(ctx, p.client, locationID, category.Term, category.Brands, category.Frozen, 0)
	})
}

func SearchIngredients(ctx context.Context, client ClientWithResponsesInterface, locationID, term string, brands []string, frozen bool, skip int) ([]Ingredient, error) {
	limit := 50
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	products, err := client.ProductSearchWithResponse(ctx, &ProductSearchParams{
		FilterLocationId: &locationID,
		FilterTerm:       &term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
	})
	if err != nil {
		return nil, fmt.Errorf("kroger product search request failed: %w", err)
	}
	if err := requireSuccess(products.StatusCode(), products.JSON500); err != nil {
		return nil, err
	}

	var ingredients []Ingredient

	for _, product := range *products.JSON200.Data {
		wildcard := len(brands) > 0 && brands[0] == "*"

		if product.Brand != nil && !slices.Contains(brands, toStr(product.Brand)) && !wildcard {
			continue
		}
		if slices.Contains(*product.Categories, "Frozen") && !frozen {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				continue
			}

			var aisle *string
			if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
				aisle = (*product.AisleLocations)[0].Number
			}

			ingredients = append(ingredients, Ingredient{
				ProductId:    product.ProductId,
				Brand:        product.Brand,
				Description:  product.Description,
				Size:         item.Size,
				PriceRegular: item.Price.Regular,
				PriceSale:    item.Price.Promo,
				Categories:   product.Categories,
				AisleNumber:  aisle,
			})
		}
	}

	if len(*products.JSON200.Data) == limit && skip < 250 {
		page, err := SearchIngredients(ctx, client, locationID, term, brands, frozen, skip+limit)
		if err == nil {
			ingredients = append(ingredients, page...)
		}
	}

	return ingredients, nil
}

func defaultStaples() []StaplesFilter {
	return append(ProduceFilters(), []StaplesFilter{
		{
			Term:   "beef",
			Brands: []string{"Simple Truth", "Kroger"},
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms", "Draper Valley", "Simple Truth"},
		},
		{
			Term: "fish",
		},
		{
			Term:   "pork",
			Brands: []string{"PORK", "Kroger", "Harris Teeter"},
		},
		{
			Term:   "shellfish",
			Brands: []string{"Sand Bar", "Kroger"},
			Frozen: true,
		},
		{
			Term:   "lamb",
			Brands: []string{"Simple Truth"},
		},
	}...)
}

func ProduceFilters() []StaplesFilter {
	return []StaplesFilter{
		{
			Term:   "fresh vegatable",
			Brands: []string{"*"},
		},
		{
			Term:   "fresh produce",
			Brands: []string{"*"},
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
