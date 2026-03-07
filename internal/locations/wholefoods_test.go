package locations

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/wholefoods"
	"context"
	"testing"
)

func TestNewAddsWholeFoodsBackendWhenEnabled(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := wholefoods.CacheStoreSummary(context.Background(), cacheStore, &wholefoods.StoreSummaryResponse{
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
	for _, backend := range locStorage.client {
		if _, ok := backend.(*wholefoods.LocationBackend); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Whole Foods backend to be registered")
	}
}
