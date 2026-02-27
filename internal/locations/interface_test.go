package locations

import (
	"careme/internal/kroger"
	"careme/internal/walmart"
)

var (
	_ locationBackend = (*kroger.ClientWithResponses)(nil)
	_ locationBackend = (*walmart.Client)(nil)
)
