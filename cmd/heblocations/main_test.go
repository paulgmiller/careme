package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"careme/internal/cache"
	"careme/internal/heb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchSummariesUsesCachedPage(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, heb.CacheStoreLocationsPage(context.Background(), cacheStore, "78204", 1, []byte(storeLocationsFixture(1, 1, storeFixture{
		StoreNumber: 699,
		Name:        "Nogalitos H-E-B",
		Address:     "1601 NOGALITOS",
		City:        "SAN ANTONIO",
		State:       "TX",
		Zip:         "78204-2427",
		Lat:         29.3978,
		Lon:         -98.51499,
	}))))

	client := &fakeStoreLocationsClient{}
	summaries, err := fetchSummaries(context.Background(), cacheStore, client, func(context.Context) (string, error) {
		return "", fmt.Errorf("build id should not be needed")
	}, "78204", 3, false)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "heb_699", summaries[0].ID)
	assert.Empty(t, client.calls)
}

func TestFetchSummariesRefreshesAndCachesPage(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	client := &fakeStoreLocationsClient{
		pages: map[int]string{
			1: storeLocationsFixture(1, 1, storeFixture{
				StoreNumber: 718,
				Name:        "South Flores Market H-E-B",
				Address:     "516 S FLORES STREET",
				City:        "SAN ANTONIO",
				State:       "TX",
				Zip:         "78204-1217",
				Lat:         29.41909,
				Lon:         -98.49648,
			}),
		},
	}

	summaries, err := fetchSummaries(context.Background(), cacheStore, client, func(context.Context) (string, error) {
		return "test-build", nil
	}, "78204", 3, true)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "heb_718", summaries[0].ID)
	assert.Equal(t, []int{1}, client.calls)

	page, err := heb.LoadCachedStoreLocationsPage(context.Background(), cacheStore, "78204", 1)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	assert.Equal(t, "heb_718", page.Summaries[0].ID)
}

func TestWriteSummariesTable(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := writeSummaries(&out, []heb.StoreSummary{{
		ID:      "heb_699",
		StoreID: "699",
		Name:    "Nogalitos H-E-B",
		Address: "1601 NOGALITOS",
		City:    "SAN ANTONIO",
		State:   "TX",
		ZipCode: "78204",
	}}, "table")
	require.NoError(t, err)
	assert.Contains(t, out.String(), "heb_699")
	assert.Contains(t, out.String(), "Nogalitos H-E-B")
}

type fakeStoreLocationsClient struct {
	pages map[int]string
	calls []int
}

func (f *fakeStoreLocationsClient) StoreLocationsPage(_ context.Context, buildID, address string, page int) ([]byte, error) {
	if buildID == "" {
		return nil, fmt.Errorf("build id is required")
	}
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}
	f.calls = append(f.calls, page)
	body := f.pages[page]
	if body == "" {
		body = storeLocationsFixture(0, page)
	}
	return []byte(body), nil
}

type storeFixture struct {
	StoreNumber int
	Name        string
	Address     string
	City        string
	State       string
	Zip         string
	Lat         float64
	Lon         float64
}

func storeLocationsFixture(total, currentPage int, stores ...storeFixture) string {
	parts := make([]string, 0, len(stores))
	for _, store := range stores {
		parts = append(parts, fmt.Sprintf(`{
			"store": {
				"longitude": %f,
				"latitude": %f,
				"storeNumber": %d,
				"name": %q,
				"address": {
					"streetAddress": %q,
					"locality": %q,
					"region": %q,
					"postalCode": %q
				}
			}
		}`, store.Lon, store.Lat, store.StoreNumber, store.Name, store.Address, store.City, store.State, store.Zip))
	}
	return fmt.Sprintf(`{
		"pageProps": {
			"currentPageStores": [%s],
			"totalStoresCount": %d,
			"currentPage": %d,
			"searchError": false
		}
	}`, strings.Join(parts, ","), total, currentPage)
}
