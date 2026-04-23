package ingredients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/recipes"
)

type stubGrader struct {
	fn func(locationHash string, ingredients []kroger.Ingredient) []ingredientgrading.Result
}

func (s stubGrader) GradeIngredients(_ context.Context, locationHash string, ingredients []kroger.Ingredient) <-chan ingredientgrading.Result {
	results := make(chan ingredientgrading.Result, len(ingredients))
	var out []ingredientgrading.Result
	if s.fn != nil {
		out = s.fn(locationHash, ingredients)
	}
	for _, result := range out {
		results <- result
	}
	close(results)
	return results
}

func (s stubGrader) PrioritizeIngredients(_ context.Context, _ string, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	return ingredients, nil
}

func TestServerReturnsIngredientsJSON(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000003", Name: "Store 1"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	description := "Honeycrisp apple"
	entries := []kroger.Ingredient{{Description: &description}}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore, stubGrader{}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash(), nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "Honeycrisp apple") {
		t.Fatalf("expected response body to include ingredient description, got %q", rr.Body.String())
	}
}

func TestServerReturnsIngredientsTSV(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000004", Name: "Store 2"},
		time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	description := "Broccoli"
	entries := []kroger.Ingredient{{Description: &description}}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore, stubGrader{}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash()+"?format=tsv", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected plain text content type, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "ProductId\tAisleNumber\tBrand\tDescription") {
		t.Fatalf("expected TSV header in response, got %q", body)
	}
	if !strings.Contains(body, "Broccoli") {
		t.Fatalf("expected TSV body to include ingredient, got %q", body)
	}
}

func TestServerReturnsNotFoundWhenParamsMissing(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	mux := http.NewServeMux()
	NewHandler(cacheStore, stubGrader{}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/missing-hash", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "parameters not found in cache") {
		t.Fatalf("expected missing params error, got %q", rr.Body.String())
	}
}

func TestServerReturnsGradedIngredientsJSON(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000005", Name: "Store 3"},
		time.Date(2026, 1, 27, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	asparagus := "Asparagus"
	chips := "Potato Chips"
	entries := []kroger.Ingredient{
		{Description: &asparagus},
		{Description: &chips},
	}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore, stubGrader{
		fn: func(locationHash string, ingredients []kroger.Ingredient) []ingredientgrading.Result {
			if locationHash != params.LocationHash() {
				t.Fatalf("unexpected location hash: %s", locationHash)
			}
			return []ingredientgrading.Result{
				{
					Ingredient: ingredients[0],
					Grade: &ai.IngredientGrade{
						SchemaVersion: "ingredient-grade-v1",
						Score:         9,
						Reason:        "Fresh and flexible.",
						Ingredient:    ai.SnapshotFromKrogerIngredient(ingredients[0]),
					},
				},
				{
					Ingredient: ingredients[1],
					Grade: &ai.IngredientGrade{
						SchemaVersion: "ingredient-grade-v1",
						Score:         2,
						Reason:        "Snack food with limited recipe value.",
						Ingredient:    ai.SnapshotFromKrogerIngredient(ingredients[1]),
					},
				},
			}
		},
	}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash()+"/graded", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"score": 9`) || !strings.Contains(body, `"description": "Asparagus"`) {
		t.Fatalf("expected high scoring asparagus in response, got %q", body)
	}
	if strings.Index(body, "Asparagus") > strings.Index(body, "Potato Chips") {
		t.Fatalf("expected results sorted by descending score, got %q", body)
	}
}
