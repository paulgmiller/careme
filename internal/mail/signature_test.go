package mail

import "careme/internal/locations"

func init() {
	locations.RegisterStaplesSignature(func(string) string { return "test-staples" })
}
