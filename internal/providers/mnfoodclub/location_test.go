package mnfoodclub

import (
	"context"
	"math"
	"testing"

	"careme/internal/locations/geo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocationsWithinDeliveryBox(t *testing.T) {
	backend := NewLocationBackend()
	for _, test := range []struct {
		name        string
		coordinates geo.Coordinate
	}{
		{"Minneapolis", geo.Coordinate{Lat: 44.98, Lon: -93.27}},
		{"St Cloud", geo.Coordinate{Lat: 45.56, Lon: -94.16}},
		{"Hudson", geo.Coordinate{Lat: 44.97, Lon: -92.76}},
		{"Rochester", geo.Coordinate{Lat: 44.01, Lon: -92.48}},
		{"southwest corner", geo.Coordinate{Lat: 43.90, Lon: -94.30}},
		{"northeast corner", geo.Coordinate{Lat: 45.65, Lon: -92.35}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := backend.GetLocationsByCoordinates(t.Context(), test.coordinates)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "MNFoodClub delivery", got[0].Name)
			assert.Equal(t, "Home delivery", got[0].Address)
			assert.Equal(t, test.coordinates, got[0].Coordinate())
			assert.True(t, backend.HasInventory(got[0].ID))
			assert.True(t, NewIdentityProvider().IsID(got[0].ID))
			lookup, err := backend.GetLocationByID(t.Context(), got[0].ID)
			require.NoError(t, err)
			assert.Equal(t, got[0].ID, lookup.ID)
			assert.Equal(t, geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}, lookup.Coordinate())
		})
	}
}

func TestLocationsOutsideDeliveryBox(t *testing.T) {
	backend := NewLocationBackend()
	for _, coordinates := range []geo.Coordinate{
		{Lat: 43.8999, Lon: -93},
		{Lat: 45.6501, Lon: -93},
		{Lat: 45, Lon: -94.3001},
		{Lat: 45, Lon: -92.3499},
		{Lat: 46.78, Lon: -92.10}, // Duluth
	} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), coordinates)
		require.NoError(t, err)
		assert.Empty(t, got)
	}
}

func TestDeliveryLocationIDsRemainStable(t *testing.T) {
	backend := NewLocationBackend()
	coordinates := geo.Coordinate{Lat: 44.98, Lon: -93.27}
	first, err := backend.GetLocationsByCoordinates(t.Context(), coordinates)
	require.NoError(t, err)
	repeated, err := backend.GetLocationsByCoordinates(t.Context(), coordinates)
	require.NoError(t, err)
	other, err := backend.GetLocationsByCoordinates(t.Context(), geo.Coordinate{Lat: 44.01, Lon: -92.48})
	require.NoError(t, err)
	assert.Equal(t, "mnfoodclub_delivery", first[0].ID)
	assert.Equal(t, first, repeated)
	assert.Equal(t, first[0].ID, other[0].ID)
	assert.Equal(t, coordinates, first[0].Coordinate())
	assert.Equal(t, geo.Coordinate{Lat: 44.01, Lon: -92.48}, other[0].Coordinate())
	lookup, err := backend.GetLocationByID(t.Context(), first[0].ID)
	require.NoError(t, err)
	assert.Equal(t, geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}, lookup.Coordinate())
}

func TestDeliveryAlias(t *testing.T) {
	backend := NewLocationBackend()
	got, err := backend.GetLocationByID(t.Context(), "mnfoodclub_delivery")
	require.NoError(t, err)
	assert.Equal(t, "mnfoodclub_delivery", got.ID)
	assert.Equal(t, geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}, got.Coordinate())
	assert.True(t, backend.HasInventory(got.ID))
}

func TestInvalidDeliveryLocationIDs(t *testing.T) {
	backend := NewLocationBackend()
	for _, id := range []string{"other_1", "mnfoodclub_", "mnfoodclub_invalid", "mnfoodclub_44.98_-93.27", "mnfoodclub_44.98_bad", "mnfoodclub_NaN_-93", "mnfoodclub_91_-93", "mnfoodclub_46_-93"} {
		got, err := backend.GetLocationByID(t.Context(), id)
		require.Error(t, err, id)
		assert.Nil(t, got)
		assert.False(t, backend.HasInventory(id), id)
	}
}

func TestInvalidDeliverySearchCoordinates(t *testing.T) {
	backend := NewLocationBackend()
	for _, coordinates := range []geo.Coordinate{{}, {Lat: math.NaN(), Lon: -93}, {Lat: 91, Lon: -93}} {
		got, err := backend.GetLocationsByCoordinates(t.Context(), coordinates)
		require.Error(t, err)
		assert.Nil(t, got)
	}
}

func TestDeliveryLocationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backend := NewLocationBackend()
	got, err := backend.GetLocationsByCoordinates(ctx, geo.Coordinate{Lat: 45, Lon: -93})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	location, err := backend.GetLocationByID(ctx, "mnfoodclub_delivery")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, location)
}
