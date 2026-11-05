package fullcircle

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"
)

const locationID = "fullcircle_delivery"

var stapleCategories = []struct{ path, name string }{
	{"/product/7/farm-product-list", "Dairy"},
	{"/product/11/farm-product-list", "Meat"},
	{"/product/20/farm-product-list", "Grains & Rice"},
}

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(id string) bool { return id == locationID }

func (identityProvider) HashForStore(id string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	date = date.AddDate(0, 0, -int((date.Weekday()-time.Monday+7)%7))
	parts := []string{"fullcircle-public-catalog-v1"}
	for _, category := range stapleCategories {
		parts = append(parts, baseURL+category.path, category.name)
	}
	signature := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
	return cachekey.NewHash(id, date, signature)
}

type ingredientClient interface {
	Fetch(context.Context, string, string) ([]ai.InputIngredient, error)
}

type staplesProvider struct {
	identityProvider
	client ingredientClient
}

func NewStaplesProvider(client ingredientClient) staplesProvider {
	return staplesProvider{client: client}
}

func (p staplesProvider) FetchStaples(ctx context.Context, id string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Full Circle location ID %q", id)
	}
	var ingredients []ai.InputIngredient
	seen := map[string]int{}
	for _, category := range stapleCategories {
		endpoint := baseURL + category.path
		items, err := p.client.Fetch(ctx, endpoint, category.name)
		if err != nil {
			return nil, fmt.Errorf("fetch Full Circle staples from %s: %w", endpoint, err)
		}
		for _, item := range items {
			if index, ok := seen[item.ProductID]; ok {
				for _, name := range item.Categories {
					if !slices.Contains(ingredients[index].Categories, name) {
						ingredients[index].Categories = append(ingredients[index].Categories, name)
					}
				}
				continue
			}
			seen[item.ProductID] = len(ingredients)
			ingredients = append(ingredients, item)
		}
	}
	return ingredients, nil
}

func (p staplesProvider) FetchWines(_ context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Full Circle location ID %q", id)
	}
	return nil, fmt.Errorf("wine lookup is not supported for location %q", id)
}
