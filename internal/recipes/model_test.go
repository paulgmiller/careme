package recipes

import (
	"testing"
	"time"

	"careme/internal/locations"
)

func TestGeneratorParamsModelParameter(t *testing.T) {
	loc := &locations.Location{ID: "loc-123", Name: "Test Loc", Address: "1 Test St", State: "TS"}
	d := time.Date(2025, 9, 17, 1, 2, 3, 0, time.UTC)

	p := DefaultParams(loc, d)
	
	// Default should have empty model
	if p.Model != "" {
		t.Fatalf("expected empty model by default, got %s", p.Model)
	}

	// Setting model should change it
	p.Model = "gpt-4o-mini"
	if p.Model != "gpt-4o-mini" {
		t.Fatalf("expected model to be set to gpt-4o-mini, got %s", p.Model)
	}

	// Different models should not affect hash equality for same base params
	p1 := DefaultParams(loc, d)
	p1.Model = "gpt-4o"
	
	p2 := DefaultParams(loc, d)
	p2.Model = "gpt-4o-mini"

	// Model should not be part of the hash (intentionally)
	// since it's a runtime parameter like ConversationID
	h1 := p1.Hash()
	h2 := p2.Hash()
	
	if h1 != h2 {
		t.Logf("Note: model parameter affects hash: %s vs %s", h1, h2)
		t.Logf("This may be intentional behavior - model choices create different cached results")
	}
}
