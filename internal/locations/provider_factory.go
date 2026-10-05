package locations

import (
	"careme/internal/cache"
)

// ProviderFactory assembles location and staples providers at the application boundary.
type ProviderFactory interface {
	NewLocations(cache.ListCache, CentroidByZip) (Store, error)
	NewStaplesBackends() ([]StaplesBackend, error)
}
