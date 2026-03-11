package albertsons

import (
	"careme/internal/cache"
	"context"
	"testing"
)

func TestStoreURLMapRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	refs := []StoreReference{
		{ID: "albertsons_611", URL: "https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html"},
		{ID: "safeway_1444", URL: "https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html"},
	}

	if err := SaveStoreURLMap(context.Background(), cacheStore, refs); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	urlMap, err := LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(urlMap) != 2 {
		t.Fatalf("expected 2 url mappings, got %d", len(urlMap))
	}
	if got := urlMap["https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html"]; got != "albertsons_611" {
		t.Fatalf("unexpected cached store id: %q", got)
	}
}

func TestStoreReferencesFromCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, &StoreSummary{
		ID:      "albertsons_611",
		Brand:   "albertsons",
		Domain:  "local.albertsons.com",
		StoreID: "611",
		Name:    "Albertsons 3710 State Line Ave",
		Address: "3710 State Line Ave",
		State:   "AR",
		ZipCode: "71854",
		URL:     "https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html",
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	refs, err := StoreReferencesFromCachedSummaries(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("StoreReferencesFromCachedSummaries returned error: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0] != (StoreReference{
		ID:  "albertsons_611",
		URL: "https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html",
	}) {
		t.Fatalf("unexpected ref: %+v", refs[0])
	}
}

func TestSaveStoreURLMapEntriesRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	urlMap := map[string]string{
		"https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html": "albertsons_611",
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html": "safeway_1444",
	}

	if err := SaveStoreURLMapEntries(context.Background(), cacheStore, urlMap); err != nil {
		t.Fatalf("SaveStoreURLMapEntries returned error: %v", err)
	}

	got, err := LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 url mappings, got %d", len(got))
	}
	if got["https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html"] != "albertsons_611" {
		t.Fatalf("unexpected albertsons mapping: %+v", got)
	}
	if got["https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html"] != "safeway_1444" {
		t.Fatalf("unexpected safeway mapping: %+v", got)
	}
}
