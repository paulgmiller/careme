package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/cache"
)

func TestSelectedChainsDefaultsToAll(t *testing.T) {
	t.Parallel()

	chains, err := selectedChains("")
	if err != nil {
		t.Fatalf("selectedChains returned error: %v", err)
	}

	seen := make(map[string]bool, len(chains))
	for _, chain := range chains {
		seen[chain.Brand] = true
	}
	for _, brand := range []string{"albertsons", "safeway", "starmarket", "haggen", "acmemarkets"} {
		if !seen[brand] {
			t.Fatalf("expected brand %q in selected chains", brand)
		}
	}
}

func TestSelectedChainsRejectsUnknownBrand(t *testing.T) {
	t.Parallel()

	if _, err := selectedChains("unknown"); err == nil {
		t.Fatal("expected unknown brand error")
	}
}

func TestSyncChainFromSitemapSkipsKnownURLsWithCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	var pageRequests atomic.Int32
	baseURL := "https://local.albertsons.test"

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case baseURL + "/sitemap.xml":
				body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/az/lake-havasu-city/1980-mcculloch-blvd.html</loc></url></urlset>`, baseURL)
				return responseWithBody(http.StatusOK, body), nil
			case baseURL + "/az/lake-havasu-city/1980-mcculloch-blvd.html":
				pageRequests.Add(1)
				return responseWithBody(http.StatusOK, `<html></html>`), nil
			default:
				return responseWithBody(http.StatusNotFound, "not found"), nil
			}
		}),
	}

	lat := 34.4839
	lon := -114.3225
	if err := albertsons.CacheStoreSummary(context.Background(), cacheStore, &albertsons.StoreSummary{
		ID:      "albertsons_3204",
		Brand:   "albertsons",
		Domain:  "local.albertsons.com",
		StoreID: "3204",
		Name:    "Albertsons 1980 Mcculloch Blvd",
		Address: "1980 Mcculloch Blvd",
		State:   "AZ",
		ZipCode: "86403",
		Lat:     &lat,
		Lon:     &lon,
		URL:     baseURL + "/az/lake-havasu-city/1980-mcculloch-blvd.html",
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	if err := albertsons.SaveStoreURLMap(context.Background(), cacheStore, map[string]string{
		baseURL + "/az/lake-havasu-city/1980-mcculloch-blvd.html": "albertsons_3204",
	}); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	chain := albertsons.Chain{
		Brand:       "albertsons",
		DisplayName: "Albertsons",
		Domain:      strings.TrimPrefix(baseURL, "https://"),
		IDPrefix:    "albertsons_",
	}

	synced, err := syncChainFromSitemap(context.Background(), cacheStore, httpClient, chain, baseURL+"/sitemap.xml", 0*time.Millisecond)
	if err != nil {
		t.Fatalf("syncChainFromSitemap returned error: %v", err)
	}
	if synced != 0 {
		t.Fatalf("expected 0 synced summaries, got %d", synced)
	}
	if pageRequests.Load() != 0 {
		t.Fatalf("expected no page requests for cached url, got %d", pageRequests.Load())
	}
	indexReader, err := cacheStore.Get(context.Background(), albertsons.LocationIndexCacheKey)
	if err != nil {
		t.Fatalf("expected compact location index: %v", err)
	}
	_ = indexReader.Close()
}

func TestSyncChainFromSitemapPreservesOtherChainURLMappings(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	baseURL := "https://local.albertsons.test"

	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case baseURL + "/sitemap.xml":
				body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>%s/ar/texarkana/3710-state-line-ave.html</loc></url></urlset>`, baseURL)
				return responseWithBody(http.StatusOK, body), nil
			case baseURL + "/ar/texarkana/3710-state-line-ave.html":
				return responseWithBody(http.StatusOK, `<!doctype html><html><head><script>window.Yext = (function(Yext){Yext.Profile = {"meta":{"id":"611"},"name":"Albertsons","address":{"city":"Texarkana","line1":"3710 State Line Ave","postalCode":"71854","region":"AR"}}; return Yext;})(window.Yext || {});</script></head><body></body></html>`), nil
			default:
				return responseWithBody(http.StatusNotFound, "not found"), nil
			}
		}),
	}

	if err := albertsons.SaveStoreURLMap(context.Background(), cacheStore, map[string]string{
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html": "safeway_1444",
	}); err != nil {
		t.Fatalf("SaveStoreURLMapEntries returned error: %v", err)
	}

	chain := albertsons.Chain{
		Brand:       "albertsons",
		DisplayName: "Albertsons",
		Domain:      strings.TrimPrefix(baseURL, "https://"),
		IDPrefix:    "albertsons_",
	}

	synced, err := syncChainFromSitemap(context.Background(), cacheStore, httpClient, chain, baseURL+"/sitemap.xml", 0*time.Millisecond)
	if err != nil {
		t.Fatalf("syncChainFromSitemap returned error: %v", err)
	}
	if synced != 1 {
		t.Fatalf("expected 1 synced summary, got %d", synced)
	}

	urlMap, err := albertsons.LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(urlMap) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(urlMap))
	}
	if got := urlMap["https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html"]; got != "safeway_1444" {
		t.Fatalf("expected safeway mapping to be preserved, got %q", got)
	}
	if got := urlMap[baseURL+"/ar/texarkana/3710-state-line-ave.html"]; got != "albertsons_611" {
		t.Fatalf("expected albertsons mapping to be added, got %q", got)
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
