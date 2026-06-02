package main

import (
	"context"
	"encoding/json"
	"testing"

	"careme/internal/aldi"
	"careme/internal/cache"
)

func TestSyncLocationsCachesSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	client := fakeSummaryClient{
		summaries: []*aldi.StoreSummary{
			{
				ID:         "aldi_F100",
				StoreID:    5757831,
				Identifier: "F100",
				Name:       "ALDI 201 W Division St",
				Address:    "201 W Division St",
				City:       "Chicago",
				State:      "IL",
				ZipCode:    "60610",
			},
		},
	}

	synced, err := syncLocations(context.Background(), cacheStore, client)
	if err != nil {
		t.Fatalf("syncLocations returned error: %v", err)
	}
	if synced != 1 {
		t.Fatalf("expected 1 synced summary, got %d", synced)
	}

	keys, err := cacheStore.List(context.Background(), aldi.StoreCachePrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "aldi_F100" {
		t.Fatalf("unexpected cached keys: %v", keys)
	}
	exists, err := cacheStore.Exists(context.Background(), aldi.LocationIndexCacheKey)
	if err != nil {
		t.Fatalf("expected compact location index: %v", err)
	}
	if !exists {
		t.Fatal("expected compact location index to exist")
	}
}

func TestSyncLocationsCachesResolvedInstoreShopID(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	client := fakeInventorySummaryClient{
		summaries: []*aldi.StoreSummary{
			{
				ID:         "aldi_F219",
				StoreID:    5767251,
				Identifier: "F219",
				Name:       "ALDI 825 S. Hurstbourne Pkwy",
				Address:    "825 S. Hurstbourne Pkwy",
				City:       "Louisville",
				State:      "KY",
				ZipCode:    "40222",
			},
		},
		shopIDs: map[string]string{"aldi_F219": "516286"},
	}

	synced, err := syncLocations(context.Background(), cacheStore, client)
	if err != nil {
		t.Fatalf("syncLocations returned error: %v", err)
	}
	if synced != 1 {
		t.Fatalf("expected 1 synced summary, got %d", synced)
	}

	reader, err := cacheStore.Get(context.Background(), aldi.StoreCachePrefix+"aldi_F219")
	if err != nil {
		t.Fatalf("read cached summary: %v", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary aldi.StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		t.Fatalf("decode cached summary: %v", err)
	}
	if summary.InstoreShopID != "516286" {
		t.Fatalf("unexpected instore shop id: %q", summary.InstoreShopID)
	}
}

type fakeSummaryClient struct {
	summaries []*aldi.StoreSummary
	err       error
}

func (f fakeSummaryClient) StoreSummaries(_ context.Context) ([]*aldi.StoreSummary, error) {
	return f.summaries, f.err
}

type fakeInventorySummaryClient struct {
	summaries []*aldi.StoreSummary
	shopIDs   map[string]string
}

func (f fakeInventorySummaryClient) StoreSummaries(_ context.Context) ([]*aldi.StoreSummary, error) {
	return f.summaries, nil
}

func (f fakeInventorySummaryClient) InStoreShopID(_ context.Context, summary *aldi.StoreSummary) (string, error) {
	return f.shopIDs[summary.ID], nil
}
