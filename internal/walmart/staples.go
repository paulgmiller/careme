package walmart

import (
	"context"
	"fmt"

	"careme/internal/kroger"
)

const UnsupportedStaplesSignature = "unsupported-staples-v1"

type StaplesProvider struct{}

func NewStaplesProvider() StaplesProvider {
	return StaplesProvider{}
}

func (p StaplesProvider) IsID(locationID string) bool {
	return (&Client{}).IsID(locationID)
}

func (p StaplesProvider) Signature() string {
	return UnsupportedStaplesSignature
}

func (p StaplesProvider) FetchStaples(_ context.Context, locationID string) ([]kroger.Ingredient, error) {
	return nil, fmt.Errorf("staples provider does not support location %q", locationID)
}
