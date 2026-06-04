package heb

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocationBackendLookupCachedStoreByID(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreSummary(context.Background(), cacheStore, robstownSummary()))

	backend, err := newLocationBackendWithDeps(context.Background(), cacheStore, staticZIPLookup{}, &fakeStoreLocationsFetcher{}, func(context.Context) (string, error) {
		return "test-build", nil
	})
	require.NoError(t, err)

	assert.True(t, backend.IsID("heb_22"))
	assert.True(t, backend.HasInventory("heb_22"))

	loc, err := backend.GetLocationByID(context.Background(), "heb_22")
	require.NoError(t, err)
	assert.Equal(t, "Robstown H-E-B", loc.Name)
	assert.Equal(t, "78380", loc.ZipCode)
	assert.Equal(t, "heb", loc.Chain)
}

func TestLocationBackendGetLocationsByZipFetchesAndCachesStoreLocations(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetcher := &fakeStoreLocationsFetcher{
		pages: map[int]string{
			1: locationSearchFixture(2, 1, locationSearchStore{
				StoreNumber: 699,
				Name:        "Nogalitos H-E-B",
				Address:     "1601 NOGALITOS",
				City:        "SAN ANTONIO",
				State:       "TX",
				Zip:         "78204-2427",
				Lat:         29.3978,
				Lon:         -98.51499,
			}),
			2: locationSearchFixture(2, 2, locationSearchStore{
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

	backend, err := newLocationBackendWithDeps(context.Background(), cacheStore, staticZIPLookup{}, fetcher, func(context.Context) (string, error) {
		return "test-build", nil
	})
	require.NoError(t, err)

	locs, err := backend.GetLocationsByZip(context.Background(), "78204")
	require.NoError(t, err)
	require.Len(t, locs, 2)
	assert.Equal(t, "heb_699", locs[0].ID)
	assert.Equal(t, "Nogalitos H-E-B", locs[0].Name)
	assert.Equal(t, "78204", locs[0].ZipCode)
	assert.Equal(t, []int{1, 2}, fetcher.calls)

	_, err = LoadCachedStoreLocationsPage(context.Background(), cacheStore, "78204", 1)
	require.NoError(t, err)

	loc, err := backend.GetLocationByID(context.Background(), "heb_718")
	require.NoError(t, err)
	assert.Equal(t, "South Flores Market H-E-B", loc.Name)
}

func TestLocationBackendGetLocationsByZipUsesCachedStoreLocations(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreLocationsPage(context.Background(), cacheStore, "78204", 1, []byte(locationSearchFixture(1, 1, locationSearchStore{
		StoreNumber: 699,
		Name:        "Nogalitos H-E-B",
		Address:     "1601 NOGALITOS",
		City:        "SAN ANTONIO",
		State:       "TX",
		Zip:         "78204-2427",
		Lat:         29.3978,
		Lon:         -98.51499,
	}))))

	fetcher := &fakeStoreLocationsFetcher{}
	backend, err := newLocationBackendWithDeps(context.Background(), cacheStore, staticZIPLookup{}, fetcher, func(context.Context) (string, error) {
		return "", fmt.Errorf("build id should not be needed")
	})
	require.NoError(t, err)

	locs, err := backend.GetLocationsByZip(context.Background(), "78204")
	require.NoError(t, err)
	require.Len(t, locs, 1)
	assert.Equal(t, "heb_699", locs[0].ID)
	assert.Empty(t, fetcher.calls)
}

type fakeStoreLocationsFetcher struct {
	pages map[int]string
	calls []int
}

func (f *fakeStoreLocationsFetcher) StoreLocationsPage(_ context.Context, buildID, address string, page int) ([]byte, error) {
	if buildID == "" {
		return nil, fmt.Errorf("build id is required")
	}
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}
	f.calls = append(f.calls, page)
	body := f.pages[page]
	if body == "" {
		body = locationSearchFixture(0, page)
	}
	return []byte(body), nil
}

type staticZIPLookup map[string]coords

type coords struct {
	Lat float64
	Lon float64
}

func (s staticZIPLookup) ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool) {
	coord, ok := s[zip]
	if !ok {
		return locationtypes.ZipCentroid{}, false
	}
	return locationtypes.ZipCentroid{Lat: coord.Lat, Lon: coord.Lon}, true
}

func robstownSummary() *StoreSummary {
	lat := 27.7912
	lon := -97.6670
	return &StoreSummary{
		ID:      "heb_22",
		StoreID: "22",
		Name:    "Robstown H-E-B",
		Address: "308 E Main",
		City:    "Robstown",
		State:   "TX",
		ZipCode: "78380",
		Lat:     &lat,
		Lon:     &lon,
	}
}

type locationSearchStore struct {
	StoreNumber int
	Name        string
	Address     string
	City        string
	State       string
	Zip         string
	Lat         float64
	Lon         float64
}

func locationSearchFixture(total, currentPage int, stores ...locationSearchStore) string {
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
