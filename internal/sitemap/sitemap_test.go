package sitemap

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/feedback"
)

const testPublicOrigin = "https://example.careme.test"

func TestHandleSitemapReturnsXMLWithFeedbackRecipeHashes(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	feedbackIO := feedback.NewIO(cacheStore)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hashes := make([]string, 0, 3)
	for i := range 3 {
		hash := fmt.Sprintf("recipe-hash-%d", i)
		if err := feedbackIO.SaveFeedback(context.Background(), hash, feedback.Feedback{
			Cooked:    true,
			Stars:     5,
			UpdatedAt: start.AddDate(0, 0, i),
		}); err != nil {
			t.Fatalf("failed to save feedback %q to cache: %v", hash, err)
		}
		hashes = append(hashes, hash)
	}

	server := New(cacheStore, testPublicOrigin)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	server.handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/xml") {
		t.Fatalf("expected XML content type, got %q", got)
	}

	var parsed urlSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid XML sitemap, got error: %v\nbody: %s", err, rr.Body.String())
	}

	expectedCount := len(hashes) + 1 // recipe URLs + static about page
	if len(parsed.URLs) != expectedCount {
		t.Fatalf("expected %d sitemap urls, got %d", expectedCount, len(parsed.URLs))
	}

	if !containsSitemapURL(parsed.URLs, testPublicOrigin+"/about") {
		t.Fatalf("missing expected static URL %q in sitemap body: %s", testPublicOrigin+"/about", rr.Body.String())
	}

	for _, hash := range hashes {
		wantURL := testPublicOrigin + "/recipe/" + hash
		if !containsSitemapURL(parsed.URLs, wantURL) {
			t.Fatalf("missing expected URL %q in sitemap body: %s", wantURL, rr.Body.String())
		}
	}
}

func TestHandleSitemapIncludesRecipePagesWithFeedback(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	params := recipes.DefaultParams(&locations.Location{
		ID:      "70005003",
		Name:    "Test Store",
		Address: "123 Test St",
	}, start)
	shoppingListHash := params.Hash()
	if err := cacheStore.Put(context.Background(), recipes.ShoppingListCachePrefix+shoppingListHash, `{"mock":"shopping-list"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	list := recipes.IO(cacheStore)
	recipe := ai.Recipe{
		Title:        "Feedback Soup",
		Description:  "A soup worth commenting on.",
		CookTime:     "35 minutes",
		CostEstimate: "$18-24",
		Ingredients:  []ai.Ingredient{{Name: "Broth", Quantity: "4 cups", Price: "$4"}},
		Instructions: []string{"Bring broth to a simmer.", "Serve hot."},
		Health:       "Balanced dinner",
		DrinkPairing: "Pinot Noir",
	}
	if err := list.SaveRecipes(context.Background(), []ai.Recipe{recipe}, shoppingListHash); err != nil {
		t.Fatalf("failed to save recipe: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	if err := list.SaveFeedback(context.Background(), recipeHash, feedback.Feedback{
		Cooked:    true,
		Stars:     5,
		Comment:   "Worth making again.",
		UpdatedAt: start,
	}); err != nil {
		t.Fatalf("failed to save feedback: %v", err)
	}

	server := New(cacheStore, testPublicOrigin)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	server.handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var parsed urlSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid XML sitemap, got error: %v\nbody: %s", err, rr.Body.String())
	}

	if len(parsed.URLs) != 2 {
		t.Fatalf("expected two URLs (about + feedback-backed recipe), got %d", len(parsed.URLs))
	}
	if !containsSitemapURL(parsed.URLs, testPublicOrigin+"/recipe/"+recipeHash) {
		t.Fatalf("missing feedback-backed recipe URL in sitemap body: %s", rr.Body.String())
	}
}

func TestHandleSitemapIncludesFeedbackWithoutCachedRecipe(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	feedbackIO := feedback.NewIO(cacheStore)
	if err := feedbackIO.SaveFeedback(context.Background(), "missing-recipe", feedback.Feedback{
		Cooked:    true,
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("failed to save feedback: %v", err)
	}

	server := New(cacheStore, testPublicOrigin)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	server.handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var parsed urlSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid XML sitemap, got error: %v\nbody: %s", err, rr.Body.String())
	}
	if len(parsed.URLs) != 2 {
		t.Fatalf("expected two URLs (about + feedback-backed recipe), got %d", len(parsed.URLs))
	}
	if !containsSitemapURL(parsed.URLs, testPublicOrigin+"/about") {
		t.Fatalf("missing expected static URL %q in sitemap body: %s", testPublicOrigin+"/about", rr.Body.String())
	}
	if !containsSitemapURL(parsed.URLs, testPublicOrigin+"/recipe/missing-recipe") {
		t.Fatalf("missing expected URL %q in sitemap body: %s", testPublicOrigin+"/recipe/missing-recipe", rr.Body.String())
	}
}

func TestHandleSitemap_IgnoresNonFeedbackKeys(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	if err := cacheStore.Put(context.Background(), recipes.ShoppingListCachePrefix+"ignored-shopping-list", `{"mock":"shopping-list"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to save shopping list key: %v", err)
	}
	if err := cacheStore.Put(context.Background(), "recipe/ignored-recipe", `{"mock":"recipe"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to save recipe key: %v", err)
	}

	server := New(cacheStore, testPublicOrigin)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	server.handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var parsed urlSet
	if err := xml.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid XML sitemap, got error: %v\nbody: %s", err, rr.Body.String())
	}
	if len(parsed.URLs) != 1 {
		t.Fatalf("expected one URL (about) with no feedback keys, got %d", len(parsed.URLs))
	}
	if parsed.URLs[0].Loc != testPublicOrigin+"/about" {
		t.Fatalf("expected only URL %q, got %q", testPublicOrigin+"/about", parsed.URLs[0].Loc)
	}
}

func TestHandleRobotsReturnsExpectedContent(t *testing.T) {
	server := New(nil, testPublicOrigin)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)

	server.handleRobots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected text content type, got %q", got)
	}
	if rr.Body.String() != fmt.Sprintf(robots, testPublicOrigin) {
		t.Fatalf("unexpected robots.txt body:\n%s", rr.Body.String())
	}
}

func containsSitemapURL(entries []urlEntry, want string) bool {
	for _, entry := range entries {
		if entry.Loc == want {
			return true
		}
	}
	return false
}
