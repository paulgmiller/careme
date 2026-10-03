package providerregistry

import (
	"context"
	"fmt"
	"net/http"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/locations"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/publix"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/providers/walmart"
	"careme/internal/providers/wegmans"
	"careme/internal/providers/wholefoods"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewLocations assembles provider backends and delegates storage to locations.
func (Factory) NewLocations(cfg *config.Config, c cache.ListCache, centroids locations.CentroidByZip) (locations.Store, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	if cfg.Mocks.Enable {
		return locations.NewMock(), nil
	}
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	factories := []locations.LocationBackendFactory{
		func(context.Context) (locations.LocationBackend, error) { return mnfoodclub.NewLocationBackend(), nil },
		func(context.Context) (locations.LocationBackend, error) {
			return smithbrothersfarms.NewLocationBackend(), nil
		},
		func(context.Context) (locations.LocationBackend, error) {
			return kroger.NewLocationBackendFromConfig(cfg, httpClient)
		},
		func(context.Context) (locations.LocationBackend, error) { return walmart.NewClient(cfg.Walmart) },
		func(ctx context.Context) (locations.LocationBackend, error) {
			return aldi.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locations.LocationBackend, error) {
			return wholefoods.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locations.LocationBackend, error) {
			return albertsons.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locations.LocationBackend, error) {
			return publix.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locations.LocationBackend, error) {
			return heb.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locations.LocationBackend, error) {
			return wegmans.NewLocationBackend(ctx, cfg, centroids)
		},
		func(context.Context) (locations.LocationBackend, error) {
			return farmersmarket.NewContainerLocationBackend()
		},
	}
	return locations.New(c, centroids, factories)
}
