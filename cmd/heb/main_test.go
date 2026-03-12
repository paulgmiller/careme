package main

import (
	"careme/internal/cache"
	"careme/internal/heb"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSyncFromSitemapSkipsKnownURLs(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	baseURL := "https://www.heb.test"
	var pageRequests atomic.Int32

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case baseURL + "/sitemap/storeSitemap.xml":
				body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/heb-store/US/tx/robstown/robstown-h-e-b-22</loc></url></urlset>`, baseURL)
				return responseWithBody(http.StatusOK, body), nil
			case baseURL + "/heb-store/US/tx/robstown/robstown-h-e-b-22":
				pageRequests.Add(1)
				return responseWithBody(http.StatusOK, `<html></html>`), nil
			default:
				return responseWithBody(http.StatusNotFound, "not found"), nil
			}
		}),
	}

	lat := 27.7912
	lon := -97.6670
	if err := heb.CacheStoreSummary(context.Background(), cacheStore, &heb.StoreSummary{
		ID:      "heb_22",
		StoreID: "22",
		Name:    "Robstown H-E-B",
		Address: "308 E Main",
		City:    "Robstown",
		State:   "TX",
		ZipCode: "78380",
		URL:     baseURL + "/heb-store/US/tx/robstown/robstown-h-e-b-22",
		Lat:     &lat,
		Lon:     &lon,
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	if err := heb.SaveStoreURLMap(context.Background(), cacheStore, map[string]string{
		baseURL + "/heb-store/US/tx/robstown/robstown-h-e-b-22": "heb_22",
	}); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	synced, err := syncFromSitemap(context.Background(), cacheStore, httpClient, baseURL+"/sitemap/storeSitemap.xml", 0*time.Millisecond)
	if err != nil {
		t.Fatalf("syncFromSitemap returned error: %v", err)
	}
	if synced != 0 {
		t.Fatalf("expected 0 synced summaries, got %d", synced)
	}
	if pageRequests.Load() != 0 {
		t.Fatalf("expected no page requests for cached url, got %d", pageRequests.Load())
	}
}

func TestSyncFromSitemapAddsNewURLMappings(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	baseURL := "https://www.heb.test"

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case baseURL + "/sitemap/storeSitemap.xml":
				body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/heb-store/US/tx/robstown/robstown-h-e-b-22</loc></url></urlset>`, baseURL)
				return responseWithBody(http.StatusOK, body), nil
			case baseURL + "/heb-store/US/tx/robstown/robstown-h-e-b-22":
				html := strings.Join([]string{
					`<!doctype html><html><head>`,
					`<title>Robstown H-E-B | 308 E MAIN | HEB.com</title>`,
					`<script type="application/ld+json">{"@context":"https://schema.org","@type":"GroceryStore","name":"Robstown H-E-B","branchCode":"22","address":{"streetAddress":"308 E Main","addressLocality":"Robstown","addressRegion":"TX","postalCode":"78380"},"geo":{"latitude":27.7912,"longitude":-97.6670}}</script>`,
					`</head><body><div>Corporate #22</div></body></html>`,
				}, "")
				return responseWithBody(http.StatusOK, html), nil
			default:
				return responseWithBody(http.StatusNotFound, "not found"), nil
			}
		}),
	}

	if err := heb.SaveStoreURLMap(context.Background(), cacheStore, map[string]string{
		"https://www.heb.com/heb-store/US/tx/austin/hancock-center-h-e-b-216": "heb_216",
	}); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	synced, err := syncFromSitemap(context.Background(), cacheStore, httpClient, baseURL+"/sitemap/storeSitemap.xml", 0*time.Millisecond)
	if err != nil {
		t.Fatalf("syncFromSitemap returned error: %v", err)
	}
	if synced != 1 {
		t.Fatalf("expected 1 synced summary, got %d", synced)
	}

	urlMap, err := heb.LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(urlMap) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(urlMap))
	}
	if got := urlMap["https://www.heb.com/heb-store/US/tx/austin/hancock-center-h-e-b-216"]; got != "heb_216" {
		t.Fatalf("expected existing mapping to be preserved, got %q", got)
	}
	if got := urlMap[baseURL+"/heb-store/US/tx/robstown/robstown-h-e-b-22"]; got != "heb_22" {
		t.Fatalf("expected new mapping to be added, got %q", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func responseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
