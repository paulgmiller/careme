package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

type stubParseLocations struct{}

func (stubParseLocations) GetLocationByID(_ context.Context, locationID string) (*locations.Location, error) {
	return &locations.Location{
		ID:      locationID,
		Name:    "Stub Store",
		Address: "1 Main St",
	}, nil
}

func TestParseQueryArgs_ParsesChoiceFields(t *testing.T) {
	ctx := context.Background()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		locServer: stubParseLocations{},
	}

	savedRecipe := ai.Recipe{
		Title:        "Saved Recipe",
		Description:  "one",
		Ingredients:  []ai.Ingredient{{Name: "a", Quantity: "1", Price: "1"}},
		Instructions: []string{"mix"},
		Health:       "ok",
		DrinkPairing: "water",
	}
	dismissedRecipe := ai.Recipe{
		Title:        "Dismissed Recipe",
		Description:  "two",
		Ingredients:  []ai.Ingredient{{Name: "b", Quantity: "2", Price: "2"}},
		Instructions: []string{"stir"},
		Health:       "ok",
		DrinkPairing: "tea",
	}

	if err := s.SaveRecipes(ctx, []ai.Recipe{savedRecipe, dismissedRecipe}, "origin"); err != nil {
		t.Fatalf("SaveRecipes() error = %v", err)
	}

	savedHash := savedRecipe.ComputeHash()
	dismissedHash := dismissedRecipe.ComputeHash()

	query := url.Values{}
	query.Set("location", "L1")
	query.Add("choice-"+savedHash, "save")
	query.Add("choice-"+dismissedHash, "dismiss")
	req := httptest.NewRequest(http.MethodGet, "/recipes?"+query.Encode(), nil)

	got, err := s.ParseQueryArgs(ctx, req)
	if err != nil {
		t.Fatalf("ParseQueryArgs() error = %v", err)
	}

	if len(got.Saved) != 1 {
		t.Fatalf("len(saved) = %d, want 1", len(got.Saved))
	}
	if got.Saved[0].ComputeHash() != savedHash {
		t.Fatalf("saved hash = %q, want %q", got.Saved[0].ComputeHash(), savedHash)
	}
	if len(got.Dismissed) != 1 {
		t.Fatalf("len(dismissed) = %d, want 1", len(got.Dismissed))
	}
	if got.Dismissed[0].ComputeHash() != dismissedHash {
		t.Fatalf("dismissed hash = %q, want %q", got.Dismissed[0].ComputeHash(), dismissedHash)
	}
}

func TestParseQueryArgs_DeduplicatesChoiceAndLegacyFields(t *testing.T) {
	ctx := context.Background()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		locServer: stubParseLocations{},
	}

	recipe := ai.Recipe{
		Title:        "Saved Recipe",
		Description:  "one",
		Ingredients:  []ai.Ingredient{{Name: "a", Quantity: "1", Price: "1"}},
		Instructions: []string{"mix"},
		Health:       "ok",
		DrinkPairing: "water",
	}

	if err := s.SaveRecipes(ctx, []ai.Recipe{recipe}, "origin"); err != nil {
		t.Fatalf("SaveRecipes() error = %v", err)
	}

	hash := recipe.ComputeHash()

	query := url.Values{}
	query.Set("location", "L1")
	query.Add("saved", hash)
	query.Add("choice-"+hash, "save")
	req := httptest.NewRequest(http.MethodGet, "/recipes?"+query.Encode(), nil)

	got, err := s.ParseQueryArgs(ctx, req)
	if err != nil {
		t.Fatalf("ParseQueryArgs() error = %v", err)
	}

	if len(got.Saved) != 1 {
		t.Fatalf("len(saved) = %d, want 1", len(got.Saved))
	}
	if got.Saved[0].ComputeHash() != hash {
		t.Fatalf("saved hash = %q, want %q", got.Saved[0].ComputeHash(), hash)
	}
}
