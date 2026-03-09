package recipes

import (
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/wholefoods"
	"context"
	"fmt"
	"strings"
)

type staplesProvider interface {
	FetchStaples(ctx context.Context, location *locations.Location) ([]kroger.Ingredient, error)
}

type routingStaplesProvider struct {
	kroger     backendStaplesProvider
	wholeFoods backendStaplesProvider
}

type backendStaplesProvider interface {
	Signature() string
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
}

func newStaplesProvider(krogerClient kroger.ClientWithResponsesInterface) staplesProvider {
	return routingStaplesProvider{
		kroger:     kroger.NewStaplesProvider(krogerClient),
		wholeFoods: wholefoods.NewStaplesProvider(wholefoods.NewClient(nil)),
	}
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, location *locations.Location) ([]kroger.Ingredient, error) {
	if location == nil {
		return nil, fmt.Errorf("location is required")
	}

	provider, err := p.providerForLocation(location.ID)
	if err != nil {
		return nil, err
	}
	return provider.FetchStaples(ctx, location.ID)
}

func staplesSignatureForLocation(location *locations.Location) string {
	if location == nil {
		return ""
	}

	switch {
	case strings.HasPrefix(location.ID, wholefoods.LocationIDPrefix):
		return wholefoods.DefaultStaplesSignature
	case strings.HasPrefix(location.ID, "walmart_"):
		return "unsupported-staples-v1"
	default:
		return kroger.DefaultStaplesSignature
	}
}

func (p routingStaplesProvider) providerForLocation(locationID string) (backendStaplesProvider, error) {
	switch {
	case strings.HasPrefix(locationID, wholefoods.LocationIDPrefix):
		if p.wholeFoods == nil {
			return nil, fmt.Errorf("whole foods staples provider not configured")
		}
		return p.wholeFoods, nil
	case strings.HasPrefix(locationID, "walmart_"):
		return nil, fmt.Errorf("staples provider does not support location %q", locationID)
	default:
		if p.kroger == nil {
			return nil, fmt.Errorf("kroger staples provider not configured")
		}
		return p.kroger, nil
	}
}
