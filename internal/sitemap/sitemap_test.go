package sitemap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	entries []sitemapEntry
	append  error
	read    error
}

func (f *fakeStore) Append(_ context.Context, entry sitemapEntry) error {
	if f.append != nil {
		return f.append
	}
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeStore) ReadAll(_ context.Context) ([]sitemapEntry, error) {
	if f.read != nil {
		return nil, f.read
	}
	out := make([]sitemapEntry, len(f.entries))
	copy(out, f.entries)
	return out, nil
}

func TestTrackShoppingListAndRender(t *testing.T) {
	store := &fakeStore{}
	h := &Server{store: store}
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

func TestRenderUsesLatestTimestampPerURL(t *testing.T) {
	store := &fakeStore{entries: []sitemapEntry{
		{URL: "/recipes?h=hash-1", LastMod: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		{URL: "/recipes?h=hash-1", LastMod: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
	}}
	h := &Server{store: store}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	h.handleSitemap(rr, req)

	body := rr.Body.String()
	if strings.Count(body, "/recipes?h=hash-1") != 1 {
		t.Fatalf("expected deduped URL entry, got body: %s", body)
	}
	if !strings.Contains(body, "2025-01-02T00:00:00Z") {
		t.Fatalf("expected latest timestamp in body: %s", body)
	}
}

func TestHandleSitemapReadError(t *testing.T) {
	h := &Server{store: &fakeStore{read: errors.New("boom")}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	h.handleSitemap(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestRegisterMountsSitemapRoute(t *testing.T) {
	store := &fakeStore{}
	h := &Server{store: store}
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

func TestParseEntries(t *testing.T) {
	input := `{"url":"/recipes?h=a","lastmod":"2025-01-02T00:00:00Z"}
{"url":"/recipes?h=b","lastmod":"2025-01-03T00:00:00Z"}
`
	entries, err := parseEntries(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}
