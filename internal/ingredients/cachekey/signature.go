package cachekey

import "testing"

var staplesSignature func(string) string

// RegisterStaplesSignature installs the provider lookup during registry initialization.
// It must be called before cache keys are used.
func RegisterStaplesSignature(signature func(string) string) {
	staplesSignature = signature
}

// StaplesSignature returns the provider version used by recipe and ingredient hashes.
// Tests without the provider registry use a fixed signature.
func StaplesSignature(locationID string) string {
	if staplesSignature != nil {
		return staplesSignature(locationID)
	}
	if testing.Testing() {
		return "test"
	}
	panic("staples signature lookup is not registered")
}
