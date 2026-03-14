package kroger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"careme/internal/parallelism"

	"github.com/samber/lo"
)

var defaultStaplesSignature = string(lo.Must(json.Marshal(defaultStaples())))

type staplesFilter struct {
	Term   string   `json:"term,omitempty"`
	Brands []string `json:"brands,omitempty"`
	Frozen bool     `json:"frozen,omitempty"`
}

type identityProvider struct{}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (p identityProvider) Signature() string {
	return defaultStaplesSignature
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
type StaplesProvider struct {
	identityProvider
	client ClientWithResponsesInterface
}

func NewStaplesProvider(client ClientWithResponsesInterface) StaplesProvider {
	return StaplesProvider{client: client}
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]Ingredient, error) {
	return parallelism.Flatten(defaultStaples(), func(category staplesFilter) ([]Ingredient, error) {
		ingredients, err := searchIngredients(ctx, p.client, locationID, category.Term, category.Brands, category.Frozen, 0)
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(ingredients), "category", category.Term, "location", locationID)
		return ingredients, err
	})
}

func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]Ingredient, error) {
	return searchIngredients(ctx, p.client, locationID, searchTerm, []string{"*"}, false, skip)
}

func searchIngredients(ctx context.Context, client ClientWithResponsesInterface, locationID, term string, brands []string, frozen bool, skip int) ([]Ingredient, error) {
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
			// InventoryStockLevel: item.InventoryStockLevel),
		}
	}

	// recursion is pretty dumb pagination
	// kroger limites us to 250
	if len(*products.JSON200.Data) == limit && skip < 250 {
		page, err := searchIngredients(ctx, client, locationID, term, brands, frozen, skip+limit)
		if err == nil {
			ingredients = append(ingredients, page...)
		}
	}

	return ingredients, nil
}

func defaultStaples() []staplesFilter {
	return append(ProduceFilters(), []staplesFilter{
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

func ProduceFilters() []staplesFilter {
	return []staplesFilter{
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

func mustJSONSignature(value any) string {
	signature, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("marshal staples signature: %w", err))
	}
	return string(signature)
}
