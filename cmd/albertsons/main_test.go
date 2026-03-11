package main

import (
	"careme/internal/albertsons"
	"careme/internal/cache"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSelectedChainsDefaultsToAll(t *testing.T) {
	t.Parallel()

	chains, err := selectedChains("")
	if err != nil {
		t.Fatalf("selectedChains returned error: %v", err)
	}
	if len(chains) != 5 {
		t.Fatalf("expected 5 chains, got %d", len(chains))
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
	if err := albertsons.SaveStoreURLMap(context.Background(), cacheStore, []albertsons.StoreReference{
		{ID: "albertsons_3204", URL: baseURL + "/az/lake-havasu-city/1980-mcculloch-blvd.html"},
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
