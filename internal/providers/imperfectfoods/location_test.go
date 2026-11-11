package imperfectfoods_test

import (
	"testing"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	"careme/internal/providers/imperfectfoods"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDemoLocationAppearsInNearbySearches(t *testing.T) {
	store, err := locations.New(cache.NewInMemoryCache(), locations.LoadCentroids(), []locations.LocationBackend{
		imperfectfoods.NewLocationBackend(),
	})
	require.NoError(t, err)
	for _, tt := range []struct {
		name       string
		coordinate geo.Coordinate
		wantCount  int
	}{
		{"98005", geo.Coordinate{Lat: 47.612771, Lon: -122.167621}, 1},
		{"distant ZIP", geo.Coordinate{Lat: 40.75, Lon: -73.99}, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			found, err := store.GetLocationsByCoordinates(t.Context(), tt.coordinate)
			require.NoError(t, err)
			require.Len(t, found, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Equal(t, "imperfectfoods_demo", found[0].ID)
				assert.Equal(t, "98005", found[0].ZipCode)
				assert.True(t, store.HasInventory(found[0].ID))
			}
		})
	}
}
