package ingredients

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerReturnsIngredientsJSON(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "loc-1", Name: "Store 1"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	description := "Honeycrisp apple"
	entries := []ai.InputIngredient{{Description: &description}}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore).Register(mux)

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
		&locations.Location{ID: "loc-2", Name: "Store 2"},
		time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	description := "Broccoli"
	entries := []ai.InputIngredient{{Description: &description}}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore).Register(mux)

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
	NewHandler(cacheStore).Register(mux)

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
