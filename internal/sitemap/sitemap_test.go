package sitemap

import (
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleSitemapReturnsXMLWithCachedRecipeHashes(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hashes := make([]string, 0, 3)
	for i := range 3 {
		loc := &locations.Location{
			ID:      fmt.Sprintf("store-%d", i),
			Name:    "Test Store",
			Address: "123 Test St",
		}
		params := recipes.DefaultParams(loc, start.AddDate(0, 0, i))
		hash := params.Hash()
		if err := cacheStore.Put(context.Background(), "shoppinglist/"+hash, `{"mock":"shopping-list"}`, cache.Unconditional()); err != nil {
			t.Fatalf("failed to save hash %q to cache: %v", hash, err)
		}
		hashes = append(hashes, hash)
	}

	server := New(cacheStore)
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

	if len(parsed.URLs) != len(hashes) {
		t.Fatalf("expected %d sitemap urls, got %d", len(hashes), len(parsed.URLs))
	}

	for _, hash := range hashes {
		wantURL := "https://careme.cooking/recipes?h=" + hash
		if !containsSitemapURL(parsed.URLs, wantURL) {
			t.Fatalf("missing expected URL %q in sitemap body: %s", wantURL, rr.Body.String())
		}
	}
}

func TestHandleSitemapNormalizesLegacyShoppingListHashToCanonical(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	params := recipes.DefaultParams(&locations.Location{ID: "store", Name: "Store"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	hash := params.Hash()

	if err := cacheStore.Put(context.Background(), "shoppinglist/"+hash, `{"mock":"legacy"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to save prefixed key: %v", err)
	}

	server := New(cacheStore)
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
		t.Fatalf("expected one URL, got %d", len(parsed.URLs))
	}
	wantURL := "https://careme.cooking/recipes?h=" + hash
	if parsed.URLs[0].Loc != wantURL {
		t.Fatalf("expected URL %q, got %q", wantURL, parsed.URLs[0].Loc)
	}
}

func TestHandleSitemap_IgnoresNonShoppingListKeys(t *testing.T) {
	t.Chdir(t.TempDir())

	cacheStore := cache.NewFileCache(".")
	params := recipes.DefaultParams(&locations.Location{ID: "store", Name: "Store"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	hash := params.Hash()

	if err := cacheStore.Put(context.Background(), hash, `{"mock":"legacy-root-shopping-list"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to save legacy root key: %v", err)
	}

	server := New(cacheStore)
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
	if len(parsed.URLs) != 0 {
		t.Fatalf("expected no URLs from non-shoppinglist keys, got %d", len(parsed.URLs))
	}
}

func TestHandleRobotsReturnsExpectedContent(t *testing.T) {
	server := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)

	server.handleRobots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected text content type, got %q", got)
	}
	if rr.Body.String() != fmt.Sprintf(robots, domain) {
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
