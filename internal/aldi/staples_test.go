package aldi

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"careme/internal/aldi/query"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderSignatureUsesStapleCategories(t *testing.T) {
	t.Parallel()

	got := NewIdentityProvider().Signature()
	want, err := json.Marshal(StapleCategories())
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestIdentityProviderIsID(t *testing.T) {
	t.Parallel()

	provider := NewIdentityProvider()
	assert.True(t, provider.IsID("aldi_F100"))
	assert.False(t, provider.IsID("publix_1847"))
	assert.False(t, provider.IsID("aldi_"))
}

func TestStapleCategoriesIncludesRequestedSlugs(t *testing.T) {
	t.Parallel()

	got := make([]string, 0, len(StapleCategories()))
	for _, category := range StapleCategories() {
		got = append(got, category.Slug)
	}

	assert.Equal(t, []string{
		"n-beef-67693",
		"n-chicken-81381",
		"n-fresh-fruits-35754",
		"n-fresh-vegetables-9190",
		"n-pork-99214",
		"n-fish-33891",
		"n-shellfish-45452",
		"n-lamb-91217",
		"n-dry-goods-pasta-19255",
	}, got)
}

func TestStaplesProviderMapsProductsToIngredients(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreSummary(t.Context(), cacheStore, nearbySummary()))
	client := &stubProductClient{
		collectionItems: map[string][]query.Item{
			"n-beef-67693": {
				{
					ID:        "items_29998-17771058",
					Name:      "Black Angus Beef",
					Size:      "1 lb",
					ProductID: "17771058",
					BrandName: "Black Angus",
					Price: query.ItemPrice{
						ViewSection: query.ItemPriceView{
							PriceValueString: "7.49",
							ItemCard:         query.PriceDisplay{PricingUnitString: "$7.49 / lb"},
						},
					},
				},
			},
		},
	}
	provider := newStaplesProviderWithCache(client, cacheStore)

	got, err := provider.FetchStaples(t.Context(), "aldi_F100")
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "17771058", got[0].ProductID)
	assert.Equal(t, "Black Angus Beef", got[0].Description)
	assert.Equal(t, "Black Angus", got[0].Brand)
	assert.Equal(t, "1 lb", got[0].Size)
	assert.Equal(t, "beef", got[0].AisleNumber)
	assert.Equal(t, []string{"beef"}, got[0].Categories)
	require.NotNil(t, got[0].PriceRegular)
	assert.InDelta(t, 7.49, *got[0].PriceRegular, 0.001)
	require.NotNil(t, got[0].PriceSale)
	assert.InDelta(t, 7.49, *got[0].PriceSale, 0.001)

	assert.Equal(t, "29998", client.storeID())
	assert.Equal(t, "60610", client.postalCode())
	slugs := client.slugs()
	slices.Sort(slugs)
	assert.Equal(t, []string{
		"n-beef-67693",
		"n-chicken-81381",
		"n-dry-goods-pasta-19255",
		"n-fish-33891",
		"n-fresh-fruits-35754",
		"n-fresh-vegetables-9190",
		"n-lamb-91217",
		"n-pork-99214",
		"n-shellfish-45452",
	}, slugs)
}

func TestStaplesProviderHydratesItemIDs(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreSummary(t.Context(), cacheStore, nearbySummary()))
	client := &stubProductClient{
		collectionItemIDs: map[string][]string{
			"n-beef-67693": {"items_29998-17771058"},
		},
		items: map[string]query.Item{
			"items_29998-17771058": {
				ID:        "items_29998-17771058",
				Name:      "Hydrated Beef",
				ProductID: "17771058",
				Price: query.ItemPrice{
					ViewSection: query.ItemPriceView{
						ItemCard: query.PriceDisplay{PriceString: "2 for $10.00"},
					},
				},
			},
		},
	}
	provider := newStaplesProviderWithCache(client, cacheStore)

	got, err := provider.FetchStaples(t.Context(), "aldi_F100")
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "Hydrated Beef", got[0].Description)
	require.NotNil(t, got[0].PriceRegular)
	assert.InDelta(t, 5.00, *got[0].PriceRegular, 0.001)
	assert.Equal(t, [][]string{{"items_29998-17771058"}}, client.itemBatches())
}

func TestStaplesProviderMissingInstoreShopID(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreSummary(t.Context(), cacheStore, farSummary()))
	provider := newStaplesProviderWithCache(&stubProductClient{}, cacheStore)

	_, err := provider.FetchStaples(t.Context(), "aldi_F216")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `ALDI location "aldi_F216" has no instore shop id`)
}

func TestStaplesProviderInvalidLocationID(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithCache(&stubProductClient{}, cache.NewInMemoryCache())

	_, err := provider.FetchStaples(t.Context(), "F100")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `ALDI location id "F100" is invalid`)
}

func TestStaplesProviderFetchWinesUnsupported(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithCache(&stubProductClient{}, cache.NewInMemoryCache())

	_, err := provider.FetchWines(t.Context(), "aldi_F100", []string{"Pinot Noir"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `wine lookup is not supported for location "aldi_F100"`)
}

type stubProductClient struct {
	mu                sync.Mutex
	collectionItems   map[string][]query.Item
	collectionItemIDs map[string][]string
	items             map[string]query.Item
	calls             []collectionCall
	itemCalls         [][]string
}

type collectionCall struct {
	storeID    string
	slug       string
	postalCode string
}

func (s *stubProductClient) CollectionProducts(_ context.Context, storeID, categorySlug string, opts query.SearchOptions) (*query.CollectionProductsPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, collectionCall{
		storeID:    storeID,
		slug:       categorySlug,
		postalCode: opts.PostalCode,
	})
	return &query.CollectionProductsPayload{
		Data: query.CollectionProductsData{
			CollectionProducts: query.CollectionProducts{
				Items: slices.Clone(s.collectionItems[categorySlug]),
			},
			CollectionProductsBasedSearchResults: query.CollectionProductsBasedSearchResults{
				ItemResultList: query.SearchItemResultList{
					ItemIDs: slices.Clone(s.collectionItemIDs[categorySlug]),
				},
			},
		},
	}, nil
}

func (s *stubProductClient) Items(_ context.Context, storeID string, ids []string, opts query.SearchOptions) (*query.ItemsPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if storeID != "29998" {
		return nil, fmt.Errorf("unexpected store id: %s", storeID)
	}
	if strings.TrimSpace(opts.PostalCode) != "60610" {
		return nil, fmt.Errorf("unexpected postal code: %s", opts.PostalCode)
	}

	s.itemCalls = append(s.itemCalls, slices.Clone(ids))
	items := make([]query.Item, 0, len(ids))
	for _, id := range ids {
		if item, ok := s.items[id]; ok {
			items = append(items, item)
		}
	}
	return &query.ItemsPayload{
		Data: query.ItemsData{Items: items},
	}, nil
}

func (s *stubProductClient) storeID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.calls) == 0 {
		return ""
	}
	return s.calls[0].storeID
}

func (s *stubProductClient) postalCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.calls) == 0 {
		return ""
	}
	return s.calls[0].postalCode
}

func (s *stubProductClient) slugs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	slugs := make([]string, 0, len(s.calls))
	for _, call := range s.calls {
		slugs = append(slugs, call.slug)
	}
	return slugs
}

func (s *stubProductClient) itemBatches() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	batches := make([][]string, 0, len(s.itemCalls))
	for _, batch := range s.itemCalls {
		batches = append(batches, slices.Clone(batch))
	}
	return batches
}
