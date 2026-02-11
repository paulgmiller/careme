package sitemap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackShoppingListAndRender(t *testing.T) {
	h := New()
	h.TrackShoppingList("hash-1")
	h.TrackShoppingList("hash-2")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	h.handleSitemap(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/xml") {
		t.Fatalf("expected application/xml content type, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "/recipes?h=hash-1") {
		t.Fatalf("expected sitemap to include hash-1 URL, body: %s", body)
	}
	if !strings.Contains(body, "/recipes?h=hash-2") {
		t.Fatalf("expected sitemap to include hash-2 URL, body: %s", body)
	}
}

func TestRegisterMountsSitemapRoute(t *testing.T) {
	h := New()
	h.TrackShoppingList("known-hash")

	mux := http.NewServeMux()
	h.Register(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "/recipes?h=known-hash") {
		t.Fatalf("expected sitemap route to serve known hash URL")
	}
}
