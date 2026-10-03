package providerregistry

import (
	"context"
	"os"
	"testing"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/providers/aldi"
)

func TestNewAddsALDIBackendWhenEnabled(t *testing.T) {
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

	listCache, err := cache.EnsureCache(aldi.Container)
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}

	lat := 41.894989
	lon := -87.629197
	if err := aldi.CacheStoreSummary(context.Background(), listCache, &aldi.StoreSummary{
		ID:            "aldi_F100",
		StoreID:       5757831,
		Identifier:    "F100",
		Name:          "ALDI 201 W Division St",
		Address:       "201 W Division St",
		City:          "Chicago",
		State:         "IL",
		ZipCode:       "60610",
		Lat:           &lat,
		Lon:           &lon,
		InstoreShopID: "5443223",
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	if err := aldi.RebuildLocationIndex(context.Background(), listCache, locations.LoadCentroids()); err != nil {
		t.Fatalf("RebuildLocationIndex returned error: %v", err)
	}

	storage, err := NewFactory(&config.Config{
		Aldi: config.AldiConfig{Enable: true},
	}).NewLocations(cacheStore, locations.LoadCentroids())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got, err := storage.GetLocationByID(context.Background(), "aldi_F100")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected provider location")
	}
}
