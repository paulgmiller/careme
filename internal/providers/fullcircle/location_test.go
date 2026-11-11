package fullcircle_test

import (
	"context"
	"math"
	"testing"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	"careme/internal/providers/fullcircle"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeliveryLocations(t *testing.T) {
	backend := fullcircle.NewLocationBackend()
	_, cacheable := backend.CachePolicy()
	assert.False(t, cacheable)
	for _, tt := range []struct {
		name  string
		point geo.Coordinate
	}{
		{"Seattle", geo.Coordinate{Lat: 47.6062, Lon: -122.3321}},
		{"Bellevue", geo.Coordinate{Lat: 47.6101, Lon: -122.2015}},
		{"Issaquah", geo.Coordinate{Lat: 47.5301, Lon: -122.0326}},
		{"Redmond", geo.Coordinate{Lat: 47.6740, Lon: -122.1215}},
		{"southwest boundary", geo.Coordinate{Lat: 47.30, Lon: -122.50}},
		{"northeast boundary", geo.Coordinate{Lat: 47.85, Lon: -121.90}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := backend.GetLocationsByCoordinates(t.Context(), tt.point)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "fullcircle_delivery", got[0].ID)
			assert.Equal(t, "Full Circle delivery", got[0].Name)
			assert.Equal(t, "Full Circle", got[0].Chain)
			assert.Equal(t, "Home delivery", got[0].Address)
			assert.Equal(t, "WA", got[0].State)
			assert.Equal(t, "https://www.fullcircle.com", got[0].URL)
			assert.Equal(t, tt.point, got[0].Coordinate())
			assert.True(t, backend.HasInventory(got[0].ID))
		})
	}
	location, err := backend.GetLocationByID(t.Context(), "fullcircle_delivery")
	require.NoError(t, err)
	assert.Equal(t, "fullcircle_delivery", location.ID)
	assert.Equal(t, geo.Coordinate{Lat: 47.61, Lon: -122.20}, location.Coordinate())
	for _, id := range []string{"", "fullcircle_123", "smithbrothersfarms_delivery"} {
		location, err := backend.GetLocationByID(t.Context(), id)
		require.ErrorContains(t, err, "invalid Full Circle location ID")
		assert.Nil(t, location)
		assert.False(t, backend.IsID(id))
		assert.False(t, backend.HasInventory(id))
	}
}

func TestDeliveryOutsideArea(t *testing.T) {
	backend := fullcircle.NewLocationBackend()
	for _, point := range []geo.Coordinate{
		{Lat: 47.2999, Lon: -122.20},
		{Lat: 47.8501, Lon: -122.20},
		{Lat: 47.61, Lon: -122.5001},
		{Lat: 47.61, Lon: -121.8999},
		{Lat: 47.2529, Lon: -122.4443}, // Tacoma
		{Lat: 45.5152, Lon: -122.6784}, // Portland
	} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), point)
		require.NoError(t, err)
		assert.Empty(t, got)
	}
}

func TestDeliveryInvalidAndCanceledRequests(t *testing.T) {
	backend := fullcircle.NewLocationBackend()
	for _, point := range []geo.Coordinate{
		{},
		{Lat: math.NaN(), Lon: -122},
		{Lat: 47, Lon: math.Inf(1)},
		{Lat: 91, Lon: -122},
		{Lat: 47, Lon: -181},
	} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), point)
		require.Error(t, err)
		assert.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := backend.GetLocationsByCoordinates(ctx, geo.Coordinate{Lat: 47.61, Lon: -122.20})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	location, err := backend.GetLocationByID(ctx, "fullcircle_delivery")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, location)
}

func TestDeliveryThroughLocationStorage(t *testing.T) {
	store, err := locations.New(cache.NewInMemoryCache(), locations.LoadCentroids(), []locations.LocationBackend{fullcircle.NewLocationBackend()})
	require.NoError(t, err)
	for _, point := range []geo.Coordinate{{Lat: 47.30, Lon: -122.50}, {Lat: 47.85, Lon: -121.90}} {
		got, err := store.GetLocationsByCoordinates(t.Context(), point)
		require.NoError(t, err)
		require.Len(t, got, 1, "delivery must survive the nearby-store distance filter")
		assert.Equal(t, point, got[0].Coordinate())
		assert.True(t, store.HasInventory(got[0].ID))
		location, err := store.GetLocationByID(t.Context(), got[0].ID)
		require.NoError(t, err)
		assert.Equal(t, geo.Coordinate{Lat: 47.61, Lon: -122.20}, location.Coordinate())
	}
}
