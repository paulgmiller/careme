package recipes

import (
	"testing"
	"time"

	"careme/internal/locations"
)

func TestGeneratorParamsHashStableForDifferentHours(t *testing.T) {
	loc := &locations.Location{ID: "loc-123", Name: "Test Loc", Address: "1 Test St", State: "TS"}
	d1 := time.Date(2025, 9, 17, 1, 2, 3, 0, time.UTC)
	d2 := time.Date(2025, 9, 17, 23, 59, 59, 0, time.UTC)

	p1 := DefaultParams(loc, d1)
	p2 := DefaultParams(loc, d2)

	h1 := p1.Hash()
	h2 := p2.Hash()

	if h1 != h2 {
		t.Fatalf("expected equal hashes for same day with different hours: got %s and %s", h1, h2)
	}

	// ensure stability across multiple calls
	if h1 != p1.Hash() {
		t.Fatalf("hash not stable across multiple calls: %s vs %s", h1, p1.Hash())
	}

	p1.Instructions = "some instructions"
	h3 := p1.Hash()
	if h3 == h1 {
		t.Fatalf("expected different hash after changing instructions: %s vs %s", h3, h1)
	}
}

func TestGeneratorParamsLocationHashStableForDifferentHours(t *testing.T) {
	loc := &locations.Location{ID: "loc-456", Name: "Another", Address: "2 Test Ave", State: "TS"}
	d1 := time.Date(2025, 9, 17, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2025, 9, 17, 12, 0, 0, 0, time.UTC)

	p1 := DefaultParams(loc, d1)
	p2 := DefaultParams(loc, d2)

	lh1 := p1.LocationHash()
	lh2 := p2.LocationHash()

	if lh1 != lh2 {
		t.Fatalf("expected equal location hashes for same day with different hours: got %s and %s", lh1, lh2)
	}

	// ensure stability across multiple calls
	if lh1 != p1.LocationHash() {
		t.Fatalf("location hash not stable across multiple calls: %s vs %s", lh1, p1.LocationHash())
	}
}
