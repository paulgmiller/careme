package locations

import (
	"careme/internal/cache"
	"careme/internal/config"
)

// ProviderFactory is implemented at the application boundary where provider
// packages can be imported without pulling them into location consumers.
type ProviderFactory interface {
	NewLocations(*config.Config, cache.ListCache, CentroidByZip) (Store, error)
	NewStaplesBackends(*config.Config) ([]StaplesBackend, error)
}
