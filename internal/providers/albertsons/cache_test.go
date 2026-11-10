package albertsons

import (
	"context"
	"testing"

	"careme/internal/cache"
)

func TestSaveStoreURLMapRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	urlMap := map[string]string{
		"https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html":  "albertsons_611",
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html": "safeway_1444",
	}

	if err := SaveStoreURLMap(context.Background(), cacheStore, urlMap); err != nil {
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
