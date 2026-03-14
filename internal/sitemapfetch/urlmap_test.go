package sitemapfetch

import (
	"careme/internal/cache"
	"context"
	"testing"
)

func TestURLMapRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	want := map[string]string{
		"https://example.com/store-1": "store_1",
		"https://example.com/store-2": "store_2",
	}

	if err := SaveURLMap(context.Background(), cacheStore, "stores/url_map.json", want); err != nil {
		t.Fatalf("SaveURLMap returned error: %v", err)
	}

	got, err := LoadURLMap(context.Background(), cacheStore, "stores/url_map.json")
	if err != nil {
		t.Fatalf("LoadURLMap returned error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d url mappings, got %d", len(want), len(got))
	}
	for url, id := range want {
		if got[url] != id {
			t.Fatalf("unexpected mapping for %q: got %q want %q", url, got[url], id)
		}
	}
}
