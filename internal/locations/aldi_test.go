package locations

import (
	"context"
	"os"
	"testing"

	"careme/internal/aldi"
	"careme/internal/cache"
	"careme/internal/config"
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
		ID:         "aldi_F100",
		StoreID:    5757831,
		Identifier: "F100",
		Name:       "ALDI 201 W Division St",
		Address:    "201 W Division St",
		City:       "Chicago",
		State:      "IL",
		ZipCode:    "60610",
		Lat:        &lat,
		Lon:        &lon,
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	storage, err := New(&config.Config{
		Aldi: config.AldiConfig{Enable: true},
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
		if _, ok := backend.(*aldi.LocationBackend); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ALDI backend to be registered")
	}
}
