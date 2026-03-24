package main

import (
	"context"
	"errors"
	"testing"

	"careme/internal/cache"
	"careme/internal/wegmans"
)

func TestSyncStoresCachesSummariesAndTracksMissing(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	client := fakeStoreClient{
		summaries: map[int]*wegmans.StoreSummary{
			1: {
				ID:          "wegmans_1",
				StoreNumber: 1,
				Name:        "Wegmans Test",
				Address:     "1 Main St",
				City:        "Testville",
				State:       "NY",
				ZipCode:     "10001",
			},
		},
		missing: map[int]bool{
			0: true,
		},
	}

	stats, err := syncStores(context.Background(), cacheStore, client, syncConfig{
		startID: 0,
		endID:   1,
	})
	if err != nil {
		t.Fatalf("syncStores returned error: %v", err)
	}
	if stats.Synced != 1 || stats.Missing != 1 || stats.Failures != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	keys, err := cacheStore.List(context.Background(), wegmans.StoreCachePrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "1" {
		t.Fatalf("unexpected cached keys: %v", keys)
	}
}

func TestSyncStoresSkipsCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := wegmans.CacheStoreSummary(context.Background(), cacheStore, &wegmans.StoreSummary{
		ID:          "wegmans_1",
		StoreNumber: 1,
		Name:        "Wegmans Test",
		Address:     "1 Main St",
		State:       "NY",
		ZipCode:     "10001",
	}); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	stats, err := syncStores(context.Background(), cacheStore, fakeStoreClient{}, syncConfig{
		startID: 1,
		endID:   1,
	})
	if err != nil {
		t.Fatalf("syncStores returned error: %v", err)
	}
	if stats.Skipped != 1 {
		t.Fatalf("expected 1 skipped store, got %+v", stats)
	}
}

type fakeStoreClient struct {
	summaries map[int]*wegmans.StoreSummary
	missing   map[int]bool
	err       error
}

func (f fakeStoreClient) StoreSummary(_ context.Context, storeNumber int) (*wegmans.StoreSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.missing[storeNumber] {
		return nil, wegmans.ErrStoreNotFound
	}
	summary, ok := f.summaries[storeNumber]
	if !ok {
		return nil, errors.New("unexpected store number")
	}
	return summary, nil
}
