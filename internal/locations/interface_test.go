package locations

import (
	"careme/internal/kroger"
	"careme/internal/walmart"
)

var (
	_ locationBackend = (*kroger.LocationBackend)(nil)
	_ locationBackend = (*walmart.Client)(nil)
)
