package providerregistry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	cachepkg "careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/smithbrothersfarms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const locationCachePrefix = "location/"

func newProviderLocationStore(t *testing.T, backend locations.LocationBackend, c cachepkg.ListCache) interface {
	GetLocationByID(context.Context, string) (*locations.Location, error)
	GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locations.Location, error)
} {
	t.Helper()
	store, err := locations.New(c, locations.LoadCentroids(), []locations.LocationBackend{backend})
	require.NoError(t, err)
	return store
}

func putProviderLocationInCache(t *testing.T, c cachepkg.ListCache, key string, location locations.Location) {
	t.Helper()
	raw, err := json.Marshal(location)
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), key, string(raw), cachepkg.Unconditional()))
}

func TestMNFoodClubLocationsByCoordinatesAreNotCached(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newProviderLocationStore(t, mnfoodclub.NewLocationBackend(), fc)
	for _, coordinates := range []geo.Coordinate{
		{Lat: 44.98, Lon: -93.27},
		{Lat: 44.01, Lon: -92.48},
	} {
		locations, err := server.GetLocationsByCoordinates(t.Context(), coordinates)
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, "mnfoodclub_delivery", locations[0].ID)
		assert.Equal(t, coordinates, locations[0].Coordinate())
	}
	assert.Never(t, func() bool {
		keys, err := fc.List(t.Context(), locationCachePrefix, "")
		require.NoError(t, err)
		return len(keys) > 0
	}, 50*time.Millisecond, time.Millisecond)
}

func TestMNFoodClubLocationByIDIgnoresCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	backend := mnfoodclub.NewLocationBackend()
	server := newProviderLocationStore(t, backend, fc)
	for _, id := range []string{"mnfoodclub_delivery", "mnfoodclub_44.98_-93.27"} {
		t.Run(id, func(t *testing.T) {
			putProviderLocationInCache(t, fc, locationCachePrefix+id, locations.Location{
				ID: id, Name: "Stale search point", Lat: new(44.98), Lon: new(-93.27),
			})
			got, err := server.GetLocationByID(t.Context(), id)
			if id == "mnfoodclub_delivery" {
				require.NoError(t, err)
				want, err := backend.GetLocationByID(t.Context(), id)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			} else {
				require.ErrorContains(t, err, "invalid MNFoodClub location ID")
				assert.Nil(t, got)
			}
		})
	}
}

func TestMNFoodClubLocationByIDDoesNotWriteCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newProviderLocationStore(t, mnfoodclub.NewLocationBackend(), fc)
	got, err := server.GetLocationByID(t.Context(), "mnfoodclub_delivery")
	require.NoError(t, err)
	assert.Equal(t, "mnfoodclub_delivery", got.ID)
	assert.Never(t, func() bool {
		exists, err := fc.Exists(t.Context(), locationCachePrefix+got.ID)
		require.NoError(t, err)
		return exists
	}, 50*time.Millisecond, time.Millisecond)
}

func TestSmithBrothersFarmsLocationsByCoordinatesAreNotCached(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newProviderLocationStore(t, smithbrothersfarms.NewLocationBackend(), fc)
	for _, coordinates := range []geo.Coordinate{
		{Lat: 47.61, Lon: -122.33},
		{Lat: 45.52, Lon: -122.68},
	} {
		locations, err := server.GetLocationsByCoordinates(t.Context(), coordinates)
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, "smithbrothersfarms_delivery", locations[0].ID)
		assert.Equal(t, coordinates, locations[0].Coordinate())
	}
	assert.Never(t, func() bool {
		keys, err := fc.List(t.Context(), locationCachePrefix, "")
		require.NoError(t, err)
		return len(keys) > 0
	}, 50*time.Millisecond, time.Millisecond)
}

func TestSmithBrothersFarmsLocationByIDIgnoresCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	backend := smithbrothersfarms.NewLocationBackend()
	server := newProviderLocationStore(t, backend, fc)
	for _, id := range []string{"smithbrothersfarms_delivery", "smithbrothersfarms_47.61_-122.33"} {
		t.Run(id, func(t *testing.T) {
			putProviderLocationInCache(t, fc, locationCachePrefix+id, locations.Location{
				ID: id, Name: "Stale search point", Lat: new(44.98), Lon: new(-93.27),
			})
			got, err := server.GetLocationByID(t.Context(), id)
			if id == "smithbrothersfarms_delivery" {
				require.NoError(t, err)
				want, err := backend.GetLocationByID(t.Context(), id)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			} else {
				require.ErrorContains(t, err, "invalid Smith Brothers Farms location ID")
				assert.Nil(t, got)
			}
		})
	}
}

func TestSmithBrothersFarmsLocationByIDDoesNotWriteCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newProviderLocationStore(t, smithbrothersfarms.NewLocationBackend(), fc)
	got, err := server.GetLocationByID(t.Context(), "smithbrothersfarms_delivery")
	require.NoError(t, err)
	assert.Equal(t, "smithbrothersfarms_delivery", got.ID)
	assert.Never(t, func() bool {
		exists, err := fc.Exists(t.Context(), locationCachePrefix+got.ID)
		require.NoError(t, err)
		return exists
	}, 50*time.Millisecond, time.Millisecond)
}
