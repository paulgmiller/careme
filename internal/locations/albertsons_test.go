package locations

import (
	"context"
	"os"
	"testing"

	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/config"
)

func TestNewAddsAlbertsonsBackendWhenEnabled(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	unsetEnvForTest(t, "AZURE_STORAGE_ACCOUNT_NAME")
	unsetEnvForTest(t, "AZURE_STORAGE_PRIMARY_ACCOUNT_KEY")

	listCache, err := cache.EnsureCache("albertsons")
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}

	lat := 47.5765527
	lon := -122.1381125
	if err := albertsons.CacheStoreSummary(context.Background(), listCache, &albertsons.StoreSummary{
		ID:      "safeway_1444",
		Brand:   "safeway",
		Domain:  "local.safeway.com",
		StoreID: "1444",
		Name:    "Safeway 15100 SE 38th St",
		Address: "15100 SE 38th St",
		State:   "WA",
		ZipCode: "98006",
		Lat:     &lat,
		Lon:     &lon,
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	storage, err := New(&config.Config{
		Albertsons: config.AlbertsonsConfig{Enable: true},
	}, cacheStore, LoadCentroids())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	locStorage, ok := storage.(*locationStorage)
	if !ok {
		t.Fatalf("expected *locationStorage, got %T", storage)
	}

	var found bool
	for _, backend := range locStorage.clients {
		if _, ok := backend.(*albertsons.LocationBackend); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Albertsons backend to be registered")
	}
}
