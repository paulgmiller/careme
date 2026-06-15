package main

import (
	"context"
	"testing"

	"careme/internal/googleads"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStoreIDs(t *testing.T) {
	assert.Equal(t, []string{"11111111", "22222222", "33333333"}, parseStoreIDs("11111111, 22222222\n33333333"))
}

func TestUniqueStoreIDsSortsAndDeduplicates(t *testing.T) {
	assert.Equal(t, []string{"11111111", "22222222"}, uniqueStoreIDs([]string{"22222222", "11111111", "22222222"}))
}

func TestHydrateTargetsRequiresCoordinates(t *testing.T) {
	_, err := hydrateTargets(context.Background(), fakeLocations{
		locations: map[string]*locationtypes.Location{
			"11111111": {ID: "11111111", Name: "No Coordinates"},
		},
	}, []string{"11111111"}, 2, "https://careme.cooking")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not include latitude/longitude")
}

func TestHydrateTargetsBuildsGoogleAdsTargets(t *testing.T) {
	lat := 47.61
	lon := -122.2
	targets, err := hydrateTargets(context.Background(), fakeLocations{
		locations: map[string]*locationtypes.Location{
			"11111111": {ID: "11111111", Name: "Kroger One", Address: "1 Main St", Lat: &lat, Lon: &lon},
		},
	}, []string{"11111111"}, 2, "https://careme.cooking")
	require.NoError(t, err)
	assert.Equal(t, []googleads.Target{{
		StoreID:     "11111111",
		StoreName:   "Kroger One",
		Address:     "1 Main St",
		LatMicro:    47610000,
		LonMicro:    -122200000,
		RadiusMiles: 2,
		FinalURL:    "https://careme.cooking/recipes?location=11111111",
	}}, targets)
}

func TestRecipeURL(t *testing.T) {
	assert.Equal(t, "https://careme.cooking/recipes?location=70100023", recipeURL("https://careme.cooking", "70100023"))
}

type fakeLocations struct {
	locations map[string]*locationtypes.Location
}

func (f fakeLocations) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	return f.locations[locationID], nil
}
