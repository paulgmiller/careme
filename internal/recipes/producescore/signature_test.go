package producescore

import "careme/internal/locations"

func init() {
	locations.RegisterStaplesSignature(func(locationID string) string { return "test-staples" })
}
