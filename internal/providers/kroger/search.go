package kroger

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/ai"
	"careme/internal/providers/kroger/products"
)

func (p staplesProvider) searchProducts(ctx context.Context, locationID, term string, brands []string) ([]products.ProductsProductModel, error) {
	const limit = 50
	var brandFilter *string
	if len(brands) > 0 {
		brandFilter = new(strings.Join(brands, "|"))
	}
	var matches []products.ProductsProductModel
	// Kroger allows page starts through 250 with 50 products per request.
	for start := 0; start <= 250; start += limit {
		response, err := p.client.ProductGetWithResponse(ctx, &products.ProductGetParams{
			FilterLocationId: &locationID, FilterTerm: &term, FilterBrand: brandFilter,
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

// StaplesSearch describes a production or experimental staples query. APIBrands
// filters brands before pagination; otherwise Brands is applied locally.
type StaplesSearch struct {
	Term      string   `json:"term"`
	Brands    []string `json:"brands,omitempty"`
	Frozen    bool     `json:"frozen,omitempty"`
	APIBrands bool     `json:"apiBrands,omitempty"`
}

// DefaultStaplesSearches returns the current production searches for comparison.
func DefaultStaplesSearches() []StaplesSearch {
	searches := make([]StaplesSearch, 0, len(defaultStaples()))
	for _, filter := range defaultStaples() {
		searches = append(searches, StaplesSearch(filter))
	}
	return searches
}

// SearchStaples evaluates one query using the same item selection as production.
// APIBrands uses Kroger's brand filter without also screening brands locally.
func (p staplesProvider) SearchStaples(ctx context.Context, locationID string, search StaplesSearch) ([]ai.InputIngredient, error) {
	var brands []string
	localBrands := search.Brands
	if search.APIBrands {
		brands = search.Brands
		localBrands = []string{"*"}
	}
	rows, err := p.searchProducts(ctx, locationID, search.Term, brands)
	if err != nil {
		return nil, err
	}
	selected := ingredientsFromProducts(rows, localBrands, search.Frozen)
	inputs := make([]ai.InputIngredient, 0, len(selected))
	for i, row := range selected {
		inputs = append(inputs, inputIngredientFromKrogerIngredient(row, i))
	}
	return inputs, nil
}
