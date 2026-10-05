package providerregistry

import (
	"careme/internal/locations"
	"careme/internal/providers/kroger"
	"careme/internal/providers/walmart"
)

var (
	_ locations.LocationBackend = (*kroger.LocationBackend)(nil)
	_ locations.LocationBackend = (*walmart.Client)(nil)
)
