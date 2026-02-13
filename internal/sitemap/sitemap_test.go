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
		if err := cacheStore.Put(context.Background(), hash, `{"mock":"shopping-list"}`, cache.Unconditional()); err != nil {
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

func containsSitemapURL(entries []urlEntry, want string) bool {
	for _, entry := range entries {
		if entry.Loc == want {
			return true
		}
	}
	return false
}
