package recipes

import (
	"context"
	"fmt"
	"testing"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/walmart"
	"careme/internal/wholefoods"
)

// todo make this a indepenedent ingredient object not kroger.
type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
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
	staplesProvider
}

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}

	return routingStaplesProvider{
		backends: defaultStaplesBackends(cfg, kclient),
	}, nil
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	return provider.FetchStaples(ctx, locationID)
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

func defaultStaplesBackends(cfg *config.Config, krogerClient kroger.ClientWithResponsesInterface) []backendStaplesProvider {
	httpClient := brightdata.NewProxyAwareHTTPClient(cfg.BrightDataProxy)

	return []backendStaplesProvider{
		kroger.NewStaplesProvider(krogerClient),
		// actowiz.NewStaplesProvider(),
		walmart.NewStaplesProvider(),
		wholefoods.NewStaplesProvider(wholefoods.NewClient(httpClient)),
		albertsons.NewStaplesProvider(cfg.Albertsons, httpClient),
	}
}

func defaultIdentityProviders() []identityProvider {
	return []identityProvider{
		kroger.NewIdentityProvider(),
		// actowiz.NewIdentityProvider(),
		albertsons.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
