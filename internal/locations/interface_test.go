package locations

import (
	"careme/internal/kroger"
	"careme/internal/walmart"
)

var (
	_ locationGetter = (*kroger.ClientWithResponses)(nil)
	_ locationGetter = (*walmart.Client)(nil)
)
