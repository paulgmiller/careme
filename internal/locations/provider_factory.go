package locations

import (
	"careme/internal/cache"
)

// ProviderFactory is implemented at the application boundary where provider
// packages can be imported without pulling them into location consumers.
type ProviderFactory interface {
	NewLocations(cache.ListCache, CentroidByZip) (Store, error)
	NewStaplesBackends() ([]StaplesBackend, error)
}
