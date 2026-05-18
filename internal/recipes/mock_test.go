package recipes

import (
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"
)

func TestMockGenerateRecipes_Returns3Recipes(t *testing.T) {
	m := NewMockGenerator(IO(cache.NewFileCache(t.TempDir())))
	loc := &locations.Location{ID: "70000002", Name: "Test Location", Address: "123 Test St", State: "TS"}
	params := DefaultParams(loc, time.Now())

	result, err := m.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
		return
	}

	if len(result.Recipes) != 3 {
		t.Errorf("expected 3 recipes, got %d", len(result.Recipes))
	}

	// Check that all recipes have required fields
	for i, recipe := range result.Recipes {
		if recipe.Title == "" {
			t.Errorf("recipe %d has empty title", i)
		}
		if recipe.Description == "" {
			t.Errorf("recipe %d has empty description", i)
		}
		if len(recipe.Ingredients) == 0 {
			t.Errorf("recipe %d has no ingredients", i)
		}
		if len(recipe.Instructions) == 0 {
			t.Errorf("recipe %d has no instructions", i)
		}
	}
}

func TestMockGenerateRecipes_ReturnsRandomRecipes(t *testing.T) {
	m := NewMockGenerator(IO(cache.NewFileCache(t.TempDir())))
	loc := &locations.Location{ID: "70000002", Name: "Test Location", Address: "123 Test St", State: "TS"}
	params := DefaultParams(loc, time.Now())

	// Generate recipes multiple times and check that we get different combinations
	// With 20 recipes choosing 3, it's very unlikely to get the same 3 in the same order multiple times
	results := make([]string, 10)
	for i := range 10 {
		result, err := m.GenerateRecipes(t.Context(), params)
		if err != nil {
			t.Fatalf("expected no error on iteration %d, got %v", i, err)
		}

		// Create a string representation of the recipe titles
		var titles strings.Builder
		for _, recipe := range result.Recipes {
			titles.WriteString(recipe.Title + "|")
		}
		results[i] = titles.String()
	}

	// Check that we got at least 2 different combinations
	// (It's statistically almost impossible to get the same 3 recipes in order 10 times)
	uniqueResults := make(map[string]bool)
	for _, res := range results {
		uniqueResults[res] = true
	}

	if len(uniqueResults) < 2 {
		t.Errorf("expected at least 2 different recipe combinations out of 10 runs, got %d", len(uniqueResults))
	}
}

func TestMockGenerateRecipes_Has20UniqueRecipes(t *testing.T) {
	if len(mockRecipes) != 20 {
		t.Errorf("expected 20 mock recipes, got %d", len(mockRecipes))
	}

	// Check that all recipes have unique titles
	titles := make(map[string]bool)
	for _, recipe := range mockRecipes {
		if titles[recipe.Title] {
			t.Errorf("duplicate recipe title found: %s", recipe.Title)
		}
		titles[recipe.Title] = true
	}
}

func TestMockGenerateRecipes_SavesReturnedRecipes(t *testing.T) {
	rio := IO(cache.NewFileCache(t.TempDir()))
	m := NewMockGenerator(rio)
	params := DefaultParams(&locations.Location{ID: "70000002", Name: "Test Location", State: "TS"}, time.Now())

	result, err := m.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if len(result.Recipes) == 0 {
		t.Fatal("expected recipes")
	}
	for _, recipe := range result.Recipes {
		got, err := rio.SingleFromCache(t.Context(), recipe.ComputeHash())
		if err != nil {
			t.Fatalf("expected recipe %q to be saved: %v", recipe.Title, err)
		}
		if got.Title != recipe.Title {
			t.Fatalf("unexpected saved recipe: got %q want %q", got.Title, recipe.Title)
		}
	}
}
