package providerregistry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/samber/lo"

	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/locations"
	locationtypes "careme/internal/locations/types"
	"careme/internal/parallelism"
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

const locationInitializationTimeout = time.Minute

// NewLocationBackends initializes enabled location providers in routing order
// with a bounded startup timeout.
func (f Factory) NewLocationBackends(centroids locations.CentroidByZip) ([]locations.LocationBackend, error) {
	cfg := f.config
	if cfg.Mocks.Enable {
		return []locations.LocationBackend{locations.NewMock()}, nil
	}
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	factories := []locationBackendFactory{
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
	return initializeLocationBackends(factories, locationInitializationTimeout)
}

type locationBackendFactory func(context.Context) (locations.LocationBackend, error)

func initializeLocationBackends(factories []locationBackendFactory, timeout time.Duration) ([]locations.LocationBackend, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results, err := parallelism.MapWithErrors(factories, func(factory locationBackendFactory) (locations.LocationBackend, error) {
		start := time.Now()
		backend, err := factory(ctx)
		if err != nil {
			if locationtypes.IsDisabledBackendError(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to initialize location backend %T: %w", backend, err)
		}
		slog.InfoContext(ctx, "initialized location backend", "backend", fmt.Sprintf("%T", backend), "latencyMS", time.Since(start).Milliseconds())
		return backend, nil
	})
	if err != nil {
		return nil, err
	}
	return lo.Compact(results), nil
}
