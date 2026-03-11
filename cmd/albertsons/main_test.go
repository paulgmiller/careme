package main

import "testing"

func TestSelectedChainsDefaultsToAll(t *testing.T) {
	t.Parallel()

	chains, err := selectedChains("")
	if err != nil {
		t.Fatalf("selectedChains returned error: %v", err)
	}
	if len(chains) != 5 {
		t.Fatalf("expected 5 chains, got %d", len(chains))
	}
}

func TestSelectedChainsRejectsUnknownBrand(t *testing.T) {
	t.Parallel()

	if _, err := selectedChains("unknown"); err == nil {
		t.Fatal("expected unknown brand error")
	}
}
