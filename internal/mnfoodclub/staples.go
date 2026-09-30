package mnfoodclub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"careme/internal/ai"
)

const LocationIDPrefix = "mnfoodclub_"

var stapleCategories = []struct {
	url   string
	pages int
}{
	{"https://mnfood.club/shop-all/produce/?sort=bestselling", 3},
	{"https://mnfood.club/shop-all/meat/", 3},
	{"https://mnfood.club/shop/pantry/pasta/", 1},
	{"https://mnfood.club/shop-all/fish-seafood/", 1},
}

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix)
}

func (identityProvider) Signature() string {
	signatureParts := make([]string, len(stapleCategories))
	for i, category := range stapleCategories {
		signatureParts[i] = category.url
	}
	for _, ingredient := range produceShareIngredients() {
		signatureParts = append(signatureParts, ingredient.Description)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(signatureParts, "\n"))))
}

type ingredientClient interface {
	Fetch(context.Context, string, int) ([]ai.InputIngredient, error)
}

// StaplesProvider serves the public catalog and static produce shares for every
// location ID beginning with mnfoodclub_.
type StaplesProvider struct {
	identityProvider
	client ingredientClient
}

func NewStaplesProvider(client ingredientClient) StaplesProvider {
	return StaplesProvider{client: client}
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	var ingredients []ai.InputIngredient
	for _, category := range stapleCategories {
		items, err := p.client.Fetch(ctx, category.url, category.pages)
		if err != nil {
			return nil, fmt.Errorf("fetch MNFoodClub staples from %s: %w", category.url, err)
		}
		ingredients = append(ingredients, items...)
	}
	return append(ingredients, produceShareIngredients()...), nil
}

// FetchWines fetches one page of wines and wine alternatives.
func (p StaplesProvider) FetchWines(ctx context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	return p.client.Fetch(ctx, "https://mnfood.club/shop/beverage/n-a-tasty-drinks/wine-wine-alternatives/", 1)
}
