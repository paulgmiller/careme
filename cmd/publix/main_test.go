package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/cache"
	"careme/internal/publix"
)

func TestSyncStoresCachesSuccessesAndMisses(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations/1083":
			http.Redirect(w, r, "/locations/1083-publix-at-university-town-center", http.StatusMovedPermanently)
		case "/locations/1083-publix-at-university-town-center":
			_, _ = w.Write([]byte(sampleStoreHTML))
		case "/locations/1084":
			http.Redirect(w, r, "/locations", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := publix.NewClientWithBaseURL(server.URL, server.Client())
	stats, err := syncStores(context.Background(), cacheStore, client, syncConfig{
		startID:       1083,
		endID:         1084,
		resumeMissing: true,
	})
	if err != nil {
		t.Fatalf("syncStores returned error: %v", err)
	}

	if stats.Synced != 1 || stats.Missing != 1 || stats.Skipped != 0 || stats.Failures != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	summaryReader, err := cacheStore.Get(context.Background(), publix.StoreCachePrefix+"1083")
	if err != nil {
		t.Fatalf("expected cached summary: %v", err)
	}
	_ = summaryReader.Close()

	missingIDs, err := publix.LoadMissingStoreIDs(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadMissingStoreIDs returned error: %v", err)
	}
	if _, ok := missingIDs["1084"]; !ok {
		t.Fatalf("expected missing store id 1084 to be cached")
	}
}

func TestSyncStoresSkipsKnownMissingAndCachedSuccesses(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := publix.SaveMissingStoreIDs(context.Background(), cacheStore, map[string]struct{}{"1084": {}}); err != nil {
		t.Fatalf("SaveMissingStoreIDs returned error: %v", err)
	}
	if err := publix.CacheStoreSummary(context.Background(), cacheStore, &publix.StoreSummary{
		ID:      "publix_1083",
		StoreID: "1083",
		Name:    "Publix at University Town Center",
		Address: "1190 University Blvd",
		City:    "Tuscaloosa",
		State:   "AL",
		ZipCode: "35401",
		URL:     "https://www.publix.com/locations/1083-publix-at-university-town-center",
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	client := publix.NewClientWithBaseURL(server.URL, server.Client())
	stats, err := syncStores(context.Background(), cacheStore, client, syncConfig{
		startID:       1083,
		endID:         1084,
		resumeMissing: true,
	})
	if err != nil {
		t.Fatalf("syncStores returned error: %v", err)
	}

	if stats.Skipped != 2 || stats.Synced != 0 || stats.Missing != 0 || stats.Failures != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if requestCount != 0 {
		t.Fatalf("expected no HTTP requests when entries are already cached, got %d", requestCount)
	}
}

func TestSyncStoresUpdatesURLMapWhenCanonicalSlugChanges(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations/1083":
			http.Redirect(w, r, "/locations/1083-new-slug", http.StatusMovedPermanently)
		case "/locations/1083-new-slug":
			_, _ = w.Write([]byte(sampleStoreHTML))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := publix.NewClientWithBaseURL(server.URL, server.Client())
	stats, err := syncStores(context.Background(), cacheStore, client, syncConfig{
		startID:       1083,
		endID:         1083,
		resumeMissing: true,
	})
	if err != nil {
		t.Fatalf("syncStores returned error: %v", err)
	}
	if stats.Synced != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

const sampleStoreHTML = `<!doctype html>
<html>
<body>
<store-details
	:store="{&quot;storeNumber&quot;:1083,&quot;type&quot;:&quot;R&quot;,&quot;name&quot;:&quot;Publix at University Town Center&quot;,&quot;address&quot;:{&quot;streetAddress&quot;:&quot;1190 University Blvd&quot;,&quot;city&quot;:&quot;Tuscaloosa&quot;,&quot;state&quot;:&quot;AL&quot;,&quot;zip&quot;:&quot;35401-1601&quot;},&quot;latitude&quot;:33.212097,&quot;longitude&quot;:-87.553585}">
</store-details>
</body>
</html>`
