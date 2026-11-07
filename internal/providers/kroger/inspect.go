package kroger

import (
	"context"
	"fmt"

	"careme/internal/ai"
	"careme/internal/providers/kroger/products"
)

// InspectStaples compares in-store product searches with the rows
// emitted by the staples backend, before shared product-ID deduplication.
// It bypasses ingredient caches and grading and retains every API item variant.
// Optional brands filter diagnostic searches on the API before pagination.
func (p staplesProvider) InspectStaples(ctx context.Context, locationID string, terms []string, brands []string) (staplesInspection, error) {
	report := staplesInspection{LocationID: locationID, Filters: defaultStaples()}
	for _, term := range terms {
		matches, err := p.searchProducts(ctx, locationID, term, brands)
		if err != nil {
			return staplesInspection{}, err
		}
		report.Searches = append(report.Searches, productInspection{Term: term, Brands: brands, Products: matches})
	}
	rows, err := p.FetchStaples(ctx, locationID)
	if err != nil {
		return staplesInspection{}, fmt.Errorf("fetch staples for comparison: %w", err)
	}
	// Only include catalog rows for products returned by the requested searches.
	ids := map[string]bool{}
	for _, search := range report.Searches {
		for _, product := range search.Products {
			ids[toStr(product.ProductId)] = true
		}
	}
	for _, row := range rows {
		if ids[row.ProductID] {
			report.Staples = append(report.Staples, row)
		}
	}
	return report, nil
}

type staplesInspection struct {
	LocationID string               `json:"locationId"`
	Filters    []staplesFilter      `json:"staplesFilters"`
	Searches   []productInspection  `json:"searches"`
	Staples    []ai.InputIngredient `json:"matchingStaplesBeforeDeduplication"`
}

type productInspection struct {
	Brands   []string                        `json:"brands,omitempty"`
	Term     string                          `json:"term"`
	Products []products.ProductsProductModel `json:"products"`
}
