package main

import (
	"context"
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
}

type fakeSummaryClient struct {
	summaries []*aldi.StoreSummary
	err       error
}

func (f fakeSummaryClient) StoreSummaries(_ context.Context) ([]*aldi.StoreSummary, error) {
	return f.summaries, f.err
}
