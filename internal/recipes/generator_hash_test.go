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

	// make sure we're intentional about breaking hash
	if h1 != "_L60CjVGjCQ" {
		t.Fatalf("expected hash to be stable and equal to _L60CjVGjCQ, got %s", h1)
	}

	legacyHash, ok := legacyRecipeHash(h1)
	if !ok {
		t.Fatal("expected current hash passhed to legacy")
	}
	if legacyHash != "cmVjaXBl_L60CjVGjCQ=" {
		t.Fatalf("expected legacy hash to be base64 of recipe hash with prefix, got %s", legacyHash)
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

func TestNormalizeLegacyRecipeHash(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "loc-legacy", Name: "Legacy Store"}, time.Date(2025, 9, 17, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	normalized, ok := normalizeLegacyRecipeHash(legacyHash)
	if !ok {
		t.Fatal("expected legacy hash normalization to succeed")
	}
	if normalized != hash {
		t.Fatalf("expected normalized hash %q, got %q", hash, normalized)
	}

	if _, ok := normalizeLegacyRecipeHash(hash); ok {
		t.Fatalf("expected canonical hash %q not to be treated as legacy", hash)
	}
}
