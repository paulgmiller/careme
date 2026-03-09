package recipes

import (
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/walmart"
	"careme/internal/wholefoods"
	"context"
	"fmt"
)

type staplesProvider interface {
	FetchStaples(ctx context.Context, location *locations.Location) ([]kroger.Ingredient, error)
}

type storeIdentityProvider interface {
	IsID(locationID string) bool
	Signature() string
}

type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

type backendStaplesProvider interface {
	IsID(locationID string) bool
	Signature() string
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
}

func newStaplesProvider(krogerClient kroger.ClientWithResponsesInterface) staplesProvider {
	return routingStaplesProvider{
		backends: defaultStaplesBackends(krogerClient),
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

func staplesSignatureForLocation(locationID string) string {
	for _, provider := range defaultStoreIdentityProviders() {
		if provider.IsID(locationID) {
			return provider.Signature()
		}
	}
	panic("unknown staples provider for location " + locationID)
}

func (p routingStaplesProvider) providerForLocation(locationID string) (backendStaplesProvider, error) {
	for _, backend := range p.backends {
		if backend.IsID(locationID) {
			return backend, nil
		}
	}
	return nil, fmt.Errorf("staples provider does not support location %q", locationID)
}

func defaultStaplesBackends(krogerClient kroger.ClientWithResponsesInterface) []backendStaplesProvider {
	return []backendStaplesProvider{
		kroger.NewStaplesProvider(krogerClient),
		wholefoods.NewStaplesProvider(wholefoods.NewClient(nil)),
		walmart.NewStaplesProvider(),
	}
}

func defaultStoreIdentityProviders() []storeIdentityProvider {
	return []storeIdentityProvider{
		kroger.NewStoreIdentityProvider(),
		wholefoods.NewStoreIdentityProvider(),
		walmart.NewStoreIdentityProvider(),
	}
}
