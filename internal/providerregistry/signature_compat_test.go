package providerregistry

import (
	"testing"
	"time"

	"careme/internal/locations"
	"careme/internal/recipes"
)

func TestKrogerRecipeHashCompatibility(t *testing.T) {
	location := &locations.Location{ID: "loc-123", Name: "Test Loc", Address: "1 Test St", State: "TS"}
	date := time.Date(2025, 9, 17, 1, 2, 3, 0, time.UTC)
	if got := recipes.DefaultParams(location, date).Hash(); got != "wrxx3dmHzBA" {
		t.Fatalf("Kroger recipe hash changed: got %s", got)
	}
}
