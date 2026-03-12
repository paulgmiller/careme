package heb

import (
	"careme/internal/cache"
	"context"
	"testing"
)

func TestSaveStoreURLMapRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	urlMap := map[string]string{
		"https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22":      "heb_22",
		"https://www.heb.com/heb-store/US/tx/austin/hancock-center-h-e-b-216": "heb_216",
	}

	if err := SaveStoreURLMap(context.Background(), cacheStore, urlMap); err != nil {
		t.Fatalf("SaveStoreURLMap returned error: %v", err)
	}

	got, err := LoadStoreURLMap(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 url mappings, got %d", len(got))
	}
	if got["https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22"] != "heb_22" {
		t.Fatalf("unexpected robstown mapping: %+v", got)
	}
}
