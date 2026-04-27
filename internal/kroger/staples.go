package kroger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"careme/internal/config"
	"careme/internal/kroger/products"
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
	client *products.ClientWithResponses
}

func NewStaplesProvider(cfg *config.Config) (*StaplesProvider, error) {
	client, err := NewProductsClientFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &StaplesProvider{client: client}, nil
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]Ingredient, error) {
	return parallelism.Flatten(defaultStaples(), func(category staplesFilter) ([]Ingredient, error) {
		ingredients, err := searchIngredients(ctx, p.client, locationID, category.Term, category.Brands, category.Frozen, 0)
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch category", "category", category.Term, "location", locationID, "error", err)
			return nil, err
		}
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(ingredients), "category", category.Term, "location", locationID)
		return ingredients, nil
	})
}

func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]Ingredient, error) {
	return searchIngredients(ctx, p.client, locationID, searchTerm, []string{"*"}, false, skip)
}

func searchIngredients(ctx context.Context, client *products.ClientWithResponses, locationID, term string, brands []string, frozen bool, skip int) ([]Ingredient, error) {
	limit := 50
	productResults, err := client.ProductGetWithResponse(ctx, &products.ProductGetParams{
		FilterLocationId: &locationID,
		FilterTerm:       &term,
		FilterLimit:      &limit,
		FilterStart:      &skip,
	})
	if err != nil {
		return nil, fmt.Errorf("kroger product search request failed: %w", err)
	}
	if err := requireSuccess(productResults.StatusCode(), productSearchErrorPayload(productResults)); err != nil {
		return nil, err
	}

	var ingredients []Ingredient

	for _, product := range *productResults.JSON200.Data {
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

			/* comeback and do in seperate change
			if item.Inventory != nil && item.Inventory.StockLevel != nil && *item.Inventory.StockLevel == TEMPORARILYOUTOFSTOCK {
				slog.WarnContext(ctx, "OOS", "description", product.Description)
				continue
			}
			*/

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
	if len(*productResults.JSON200.Data) == limit && skip < 250 {
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
