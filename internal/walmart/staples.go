package walmart

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/ai"
)

const UnsupportedStaplesSignature = "unsupported-staples-v1"

type identityProvider struct{}

func NewStaplesProvider() StaplesProvider {
	return StaplesProvider{}
}

type StaplesProvider struct {
	identityProvider
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (c identityProvider) IsID(locationID string) bool {
	const prefix = "walmart_"
	if !strings.HasPrefix(locationID, prefix) {
		return false
	}
	if len(locationID) == len(prefix) {
		return false
	}
	for i := len(prefix); i < len(locationID); i++ {
		if locationID[i] < '0' || locationID[i] > '9' {
			return false
		}
	}
	return true
}

func (*identityProvider) HasInventory(locationID string) bool {
	return false
}

func (p identityProvider) Signature() string {
	return UnsupportedStaplesSignature
}

func (p StaplesProvider) FetchStaples(_ context.Context, locationID string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("staples provider does not support location %q", locationID)
}

func (p StaplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", locationID)
}
