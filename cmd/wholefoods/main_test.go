package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"careme/internal/cache"
	"careme/internal/wholefoods"
)

func TestResolveStoreReferencesFillsMissingCachedSitemapEntries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			if _, err := fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/stores/westlake</loc></url><url><loc>%s/stores/greenville</loc></url></urlset>`, server.URL, server.URL); err != nil {
				t.Fatalf("Fprintf returned error: %v", err)
			}
		case "/stores/greenville":
			if _, err := fmt.Fprint(w, `<div store-id="10224"></div>`); err != nil {
				t.Fatalf("Fprint returned error: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := wholefoods.SaveStoreURLMap(context.Background(), cacheStore, []wholefoods.StoreReference{
		{ID: "10216", URL: server.URL + "/stores/westlake"},
	}); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	refs, err := resolveStoreReferences(context.Background(), cacheStore, server.Client(), server.URL+"/sitemap.xml")
	if err != nil {
		t.Fatalf("resolveStoreReferences returned error: %v", err)
	}

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0] != (wholefoods.StoreReference{ID: "10216", URL: server.URL + "/stores/westlake"}) {
		t.Fatalf("unexpected first ref: %+v", refs[0])
	}
	if refs[1] != (wholefoods.StoreReference{ID: "10224", URL: server.URL + "/stores/greenville"}) {
		t.Fatalf("unexpected second ref: %+v", refs[1])
	}

	urlMap, err := wholefoods.LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if got := urlMap[server.URL+"/stores/greenville"]; got != "10224" {
		t.Fatalf("expected greenville to be added to cache, got %q", got)
	}
}

func TestResolveStoreReferencesChecksSitemapEvenWithCompleteCache(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	var sitemapRequests atomic.Int32
	var pageRequests atomic.Int32

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			sitemapRequests.Add(1)
			if _, err := fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/stores/westlake</loc></url></urlset>`, server.URL); err != nil {
				t.Fatalf("Fprintf returned error: %v", err)
			}
		case "/stores/westlake":
			pageRequests.Add(1)
			if _, err := fmt.Fprint(w, `<div store-id="10216"></div>`); err != nil {
				t.Fatalf("Fprint returned error: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := wholefoods.SaveStoreURLMap(context.Background(), cacheStore, []wholefoods.StoreReference{
		{ID: "10216", URL: server.URL + "/stores/westlake"},
	}); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	refs, err := resolveStoreReferences(context.Background(), cacheStore, server.Client(), server.URL+"/sitemap.xml")
	if err != nil {
		t.Fatalf("resolveStoreReferences returned error: %v", err)
	}

	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if sitemapRequests.Load() != 1 {
		t.Fatalf("expected sitemap to be requested once, got %d", sitemapRequests.Load())
	}
	if pageRequests.Load() != 0 {
		t.Fatalf("expected no store page requests when cache is complete, got %d", pageRequests.Load())
	}
}
