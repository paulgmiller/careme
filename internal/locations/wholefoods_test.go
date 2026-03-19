package locations

import (
	"context"
	"os"
	"testing"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/wholefoods"
)

func TestNewAddsWholeFoodsBackendWhenEnabled(t *testing.T) {
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

	listCache, err := cache.EnsureCache("wholefoods")
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}
	if err := wholefoods.CacheStoreSummary(context.Background(), listCache, &wholefoods.StoreSummaryResponse{
		StoreID:     10216,
		DisplayName: "Westlake",
		PrimaryLocation: wholefoods.StoreLocation{
			Address: wholefoods.StoreAddress{
				StreetAddressLine1: "2210 Westlake Ave",
				State:              "WA",
				ZipCode:            "98121",
			},
			Latitude:  47.618249,
			Longitude: -122.337898,
		},
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	storage, err := New(&config.Config{
		WholeFoods: config.WholeFoodsConfig{Enable: true},
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
		if _, ok := backend.(*wholefoods.LocationBackend); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Whole Foods backend to be registered")
	}
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()

	value, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q) returned error: %v", key, err)
	}
	if ok {
		t.Cleanup(func() {
			_ = os.Setenv(key, value)
		})
	}
}
