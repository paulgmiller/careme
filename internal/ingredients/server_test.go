package ingredients

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/recipes"
)

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
	NewHandler(cacheStore).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash()+"?format=json", nil)
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

func TestServerReturnsIngredientInspectorHTML(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000003", Name: "Store 1"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	apple := "Honeycrisp apple"
	bread := "Sourdough loaf"
	entries := []kroger.Ingredient{
		{Description: &apple},
		{Description: &bread},
	}
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
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Ingredient Coverage Inspector") {
		t.Fatalf("expected inspector title, got %q", body)
	}
	if !strings.Contains(body, "Honeycrisp apple") {
		t.Fatalf("expected matched ingredient in body, got %q", body)
	}
	if !strings.Contains(body, "Produce") {
		t.Fatalf("expected produce section in body, got %q", body)
	}
	if !strings.Contains(body, "/admin/ingredients/"+params.Hash()) {
		t.Fatalf("expected admin-prefixed ingredient links in body, got %q", body)
	}
	if !strings.Contains(body, "view=unmatched") {
		t.Fatalf("expected unmatched view link in body, got %q", body)
	}
}

func TestServerReturnsGlobalUnmatchedHTML(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000003", Name: "Store 1"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	apple := "Honeycrisp apple"
	bread := "Sourdough loaf"
	salmon := "Atlantic salmon fillet"
	entries := []kroger.Ingredient{
		{Description: &apple},
		{Description: &bread},
		{Description: &salmon},
	}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash()+"?view=unmatched", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Unmatched Across All Categories") {
		t.Fatalf("expected global unmatched heading, got %q", body)
	}
	if !strings.Contains(body, "Sourdough loaf") {
		t.Fatalf("expected globally unmatched ingredient in body, got %q", body)
	}
	if strings.Contains(body, "Honeycrisp apple") && !strings.Contains(body, "Matched anywhere") {
		t.Fatalf("expected matched ingredients to stay out of unmatched list, got %q", body)
	}
	if strings.Contains(body, "Atlantic salmon fillet") && !strings.Contains(body, "Matched anywhere") {
		t.Fatalf("expected seafood match to stay out of unmatched list, got %q", body)
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

func TestServerRejectsUnknownDataset(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	params := recipes.DefaultParams(
		&locations.Location{ID: "70000005", Name: "Store 3"},
		time.Date(2026, 1, 27, 0, 0, 0, 0, time.UTC),
	)
	if err := rio.SaveParams(t.Context(), params); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	description := "Salmon fillet"
	entries := []kroger.Ingredient{{Description: &description}}
	if err := rio.SaveIngredients(t.Context(), params.LocationHash(), entries); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(cacheStore).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/ingredients/"+params.Hash()+"?dataset=unknown", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unknown dataset") {
		t.Fatalf("expected unknown dataset error, got %q", rr.Body.String())
	}
}
