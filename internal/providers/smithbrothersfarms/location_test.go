package smithbrothersfarms

import (
	"context"
	"math"
	"testing"

	"careme/internal/locations/geo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeliveryLocations(t *testing.T) {
	backend := NewLocationBackend()
	assert.False(t, backend.IsCacheable())
	for _, point := range []geo.Coordinate{
		{Lat: 47.61, Lon: -122.33}, // Seattle
		{Lat: 47.04, Lon: -122.90}, // Olympia is beyond the tighter western boundary.
		{Lat: 47.25, Lon: -122.44}, // Tacoma
		{Lat: 45.52, Lon: -122.68}, // Portland
		{Lat: 45.64, Lon: -122.66}, // Vancouver
		{Lat: 46.95, Lon: -122.80},
		{Lat: 48.05, Lon: -121.90},
		{Lat: 45.30, Lon: -122.95},
		{Lat: 45.80, Lon: -122.35},
	} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), point)
		require.NoError(t, err)
		if point.Lon == -122.90 {
			assert.Empty(t, got)
			continue
		}
		require.Len(t, got, 1)
		assert.Equal(t, "smithbrothersfarms_delivery", got[0].ID)
		assert.Equal(t, "Smith Brothers Farms delivery", got[0].Name)
		assert.Equal(t, "Smith Brothers Farms", got[0].Chain)
		assert.Equal(t, "Home delivery", got[0].Address)
		assert.Equal(t, point, got[0].Coordinate())
		assert.True(t, backend.HasInventory(got[0].ID))
	}
	location, err := backend.GetLocationByID(t.Context(), "smithbrothersfarms_delivery")
	require.NoError(t, err)
	assert.Equal(t, geo.Coordinate{Lat: 47.365, Lon: -122.233}, location.Coordinate())
	for _, id := range []string{"other_delivery", "smithbrothersfarms_", "smithbrothersfarms_47_-122"} {
		got, err := backend.GetLocationByID(t.Context(), id)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.False(t, backend.HasInventory(id))
	}
}

func TestDeliveryOutsideAndInvalidCoordinates(t *testing.T) {
	backend := NewLocationBackend()
	for _, point := range []geo.Coordinate{
		{Lat: 46.5, Lon: -122.5}, // Gap between the boxes.
		{Lat: 46.9499, Lon: -122.3},
		{Lat: 48.0501, Lon: -122.3},
		{Lat: 47.5, Lon: -122.8001},
		{Lat: 47.5, Lon: -121.8999},
		{Lat: 45.2999, Lon: -122.6},
		{Lat: 45.8001, Lon: -122.6},
		{Lat: 45.5, Lon: -122.9501},
		{Lat: 45.5, Lon: -122.3499},
	} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), point)
		require.NoError(t, err)
		assert.Empty(t, got)
	}
	for _, point := range []geo.Coordinate{{}, {Lat: math.NaN(), Lon: -122}, {Lat: 47, Lon: math.Inf(1)}, {Lat: 91, Lon: -122}} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), point)
		require.Error(t, err)
		assert.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := backend.GetLocationsByCoordinates(ctx, geo.Coordinate{Lat: 47.6, Lon: -122.3})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	location, err := backend.GetLocationByID(ctx, "smithbrothersfarms_delivery")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, location)
}
