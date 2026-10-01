package locations

import (
	"careme/internal/providers/kroger"
	"careme/internal/providers/walmart"
)

var (
	_ locationBackend = (*kroger.LocationBackend)(nil)
	_ locationBackend = (*walmart.Client)(nil)
)
