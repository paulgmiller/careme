package locations

import (
	"context"
	"os"
	"testing"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/heb"
)

func TestNewAddsHEBBackendWhenEnabled(t *testing.T) {
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

	listCache, err := cache.EnsureCache("heb")
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}

	lat := 27.7912
	lon := -97.6670
	if err := heb.CacheStoreSummary(context.Background(), listCache, &heb.StoreSummary{
		ID:      "heb_22",
		StoreID: "22",
		Name:    "Robstown H-E-B",
		Address: "308 E Main",
		City:    "Robstown",
		State:   "TX",
		ZipCode: "78380",
		Lat:     &lat,
		Lon:     &lon,
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	storage, err := New(&config.Config{
		HEB: config.HEBConfig{Enable: true},
	}, cacheStore, LoadCentroids())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	locStorage, ok := storage.(*locationStorage)
	if !ok {
		t.Fatalf("expected *locationStorage, got %T", storage)
	}

	var found bool
	for _, backend := range locStorage.client {
		if _, ok := backend.(*heb.LocationBackend); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected HEB backend to be registered")
	}
}
