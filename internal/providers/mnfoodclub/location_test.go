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
			require.Len(t, got, 3)
			for i, want := range []struct{ id, name, url string }{
				{"mnfoodclub_delivery", "MN Food Club", "https://mnfood.club/"},
				{"mnfoodclub_tcfarm_delivery", "TC Farm", "https://tc.farm/"},
				{"mnfoodclub_nelsongrassfarm_delivery", "Nelson Grass Farm", "https://nelsongrassfarm.com/"},
			} {
				location := got[i]
				assert.Equal(t, want.id, location.ID)
				assert.Equal(t, want.name+" delivery", location.Name)
				assert.Equal(t, want.name, location.Chain)
				assert.Equal(t, want.url, location.URL)
				assert.Equal(t, "Home delivery", location.Address)
				assert.Equal(t, test.coordinates, location.Coordinate())
				assert.True(t, backend.HasInventory(location.ID))
				assert.True(t, NewIdentityProvider().IsID(location.ID))
				lookup, err := backend.GetLocationByID(t.Context(), location.ID)
				require.NoError(t, err)
				assert.Equal(t, location.ID, lookup.ID)
				assert.Equal(t, location.Name, lookup.Name)
				assert.Equal(t, location.URL, lookup.URL)
				assert.Equal(t, location.Chain, lookup.Chain)
				assert.Equal(t, geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}, lookup.Coordinate())
			}
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
	require.Len(t, first, 3)
	require.Len(t, other, 3)
	for i, location := range first {
		assert.Equal(t, location.ID, other[i].ID)
		assert.Equal(t, coordinates, location.Coordinate())
		assert.Equal(t, geo.Coordinate{Lat: 44.01, Lon: -92.48}, other[i].Coordinate())
	}
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
