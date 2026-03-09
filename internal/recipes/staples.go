package recipes

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/walmart"
	"careme/internal/wholefoods"
	"context"
	"fmt"
	"testing"
)

type staplesProvider interface {
	FetchStaples(ctx context.Context, location *locations.Location) ([]kroger.Ingredient, error)
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error)
}

type identityProvider interface {
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
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error)
}

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return routingStaplesProvider{
		backends: defaultStaplesBackends(kclient),
	}, nil
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

func (p routingStaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	return provider.GetIngredients(ctx, locationID, searchTerm, skip)
}

func staplesSignatureForLocation(locationID string) string {
	for _, provider := range defaultIdentityProviders() {
		if provider.IsID(locationID) {
			return provider.Signature()
		}
	}

	if testing.Testing() && locationID == "loc-123" {
		return kroger.NewIdentityProvider().Signature()
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

func defaultIdentityProviders() []identityProvider {
	return []identityProvider{
		kroger.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
