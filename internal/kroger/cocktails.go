package kroger

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/kroger/products"
	"careme/internal/seasons"
)

// CocktailProvider searches its own catalog, independently of dinner staples.
type CocktailProvider struct{ client *products.ClientWithResponses }

func NewCocktailProvider(cfg *config.Config, httpClient *http.Client) (*CocktailProvider, error) {
	client, err := NewProductsClientFromConfig(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	return &CocktailProvider{client: client}, nil
}

func cocktailTerms(season seasons.Season) []string {
	terms := []string{"gin", "vodka", "rum", "tequila", "bourbon", "orange liqueur", "cocktail bitters", "club soda", "tonic water", "ginger beer", "lemon", "lime", "fresh mint", "fresh rosemary", "fresh basil"}
	switch season {
	case seasons.Spring:
		return append(terms, "strawberries", "rhubarb", "cucumber")
	case seasons.Summer:
		return append(terms, "peaches", "watermelon", "blackberries")
	case seasons.Fall:
		return append(terms, "apple cider", "pears", "cranberries")
	default:
		return append(terms, "grapefruit", "oranges", "pomegranate")
	}
}

func (p *CocktailProvider) FetchCocktailIngredients(ctx context.Context, locationID string, season seasons.Season) ([]ai.InputIngredient, error) {
	var result []ai.InputIngredient
	seen := map[string]bool{}
	for _, term := range cocktailTerms(season) {
		limit := 12
		resp, err := p.client.ProductGetWithResponse(ctx, &products.ProductGetParams{FilterLocationId: &locationID, FilterTerm: &term, FilterLimit: &limit, FilterFulfillment: &availableInStore})
		if err != nil {
			return nil, fmt.Errorf("search cocktail ingredient %s: %w", term, err)
		}
		if err := requireSuccess(resp.StatusCode(), productSearchErrorPayload(resp)); err != nil {
			return nil, fmt.Errorf("search %s: %w", term, err)
		}
		if resp.JSON200 == nil || resp.JSON200.Data == nil {
			return nil, fmt.Errorf("missing Kroger catalog for %s", term)
		}
		for _, product := range *resp.JSON200.Data {
			if product.ProductId == nil || product.Description == nil || product.Items == nil {
				continue
			}
			id := strings.TrimSpace(*product.ProductId)
			if id == "" || seen[id] {
				continue
			}
			for _, item := range *product.Items {
				if item.Inventory != nil && item.Inventory.StockLevel != nil && *item.Inventory.StockLevel == products.TEMPORARILYOUTOFSTOCK {
					continue
				}
				if item.Price == nil {
					continue
				}
				result = append(result, inputIngredientFromKrogerIngredient(Ingredient{ProductId: product.ProductId, Description: product.Description, Brand: product.Brand, Categories: product.Categories, Size: item.Size, PriceRegular: item.Price.Regular, PriceSale: item.Price.Promo}, 0))
				seen[id] = true
				break
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no cocktail ingredients found at this Kroger store")
	}
	slices.SortFunc(result, func(a, b ai.InputIngredient) int { return strings.Compare(a.ProductID, b.ProductID) })
	return result, nil
}
