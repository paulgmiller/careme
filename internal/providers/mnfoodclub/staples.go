package mnfoodclub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"careme/internal/ai"
)

const LocationIDPrefix = "mnfoodclub_"

const catalogVersion = "mnfoodclub-bigcommerce-v1"

// The existing storefront's Wine & Wine Alternatives category.
const wineCategoryID = 178

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix)
}

func (identityProvider) Signature() string {
	signatureParts := []string{catalogVersion}
	for _, ingredient := range produceShareIngredients() {
		signatureParts = append(signatureParts, ingredient.Description)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(signatureParts, "\n"))))
}

type ingredientClient interface {
	Fetch(context.Context, int) ([]ai.InputIngredient, error)
}

// StaplesProvider serves the API catalog and static produce shares for every
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
	ingredients, err := p.client.Fetch(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("fetch MNFoodClub staples: %w", err)
	}
	return append(ingredients, produceShareIngredients()...), nil
}

// FetchWines fetches every page of wines and wine alternatives.
func (p StaplesProvider) FetchWines(ctx context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	wines, err := p.client.Fetch(ctx, wineCategoryID)
	if err != nil {
		return nil, fmt.Errorf("fetch MNFoodClub wines: %w", err)
	}
	return wines, nil
}
