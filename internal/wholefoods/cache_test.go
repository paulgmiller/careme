package wholefoods

import (
	"careme/internal/cache"
	"context"
	"testing"
)

func TestStoreURLMapRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	refs := []StoreReference{
		{ID: "10216", URL: "https://www.wholefoodsmarket.com/stores/westlake"},
		{ID: "10224", URL: "https://www.wholefoodsmarket.com/stores/greenville"},
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
	if got := urlMap["https://www.wholefoodsmarket.com/stores/westlake"]; got != "10216" {
		t.Fatalf("unexpected cached store id: %q", got)
	}
}
