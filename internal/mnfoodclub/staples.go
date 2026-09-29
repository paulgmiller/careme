package mnfoodclub

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/ai"
)

const LocationIDPrefix = "mnfoodclub_"

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix)
}

func (identityProvider) Signature() string {
	return "mnfoodclub-staples-pages-1-3-v1"
}

type ingredientClient interface {
	FetchIngredients(context.Context) ([]ai.InputIngredient, error)
}

// StaplesProvider serves the same public produce and meat catalog for every
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
	return p.client.FetchIngredients(ctx)
}

// FetchWines returns no candidates: the sourced catalog contains produce and meat.
func (p StaplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	return nil, nil
}
