package providerregistry

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
)

type Factory struct{}

var _ locations.ProviderFactory = Factory{}

func (Factory) NewLocations(cfg *config.Config, c cache.ListCache, centroids locations.CentroidByZip) (locations.Store, error) {
	return NewLocations(cfg, c, centroids)
}

func (Factory) NewStaplesBackends(cfg *config.Config) ([]locations.StaplesBackend, error) {
	return NewStaplesBackends(cfg)
}
