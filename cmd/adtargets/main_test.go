package main

import (
	"bytes"
	"context"
	"testing"

	"careme/internal/campaigns"
	"careme/internal/config"
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

func TestAdvertisedRecipeStoreIDsDefaultsToCampaignLocations(t *testing.T) {
	expected := make([]string, 0, len(campaigns.AdvertisedRecipeLocations()))
	for _, advertised := range campaigns.AdvertisedRecipeLocations() {
		expected = append(expected, advertised.Location.ID)
	}

	assert.Len(t, advertisedRecipeStoreIDs(), len(campaigns.AdvertisedRecipeLocations()))
	assert.Equal(t, expected, advertisedRecipeStoreIDs())
}

func TestDefaultAdsIDs(t *testing.T) {
	assert.Equal(t, "5812848025", normalizeCustomerID(defaultCustomerID))
	assert.Equal(t, "23939758740", defaultCampaignID)
}

func TestNewAdTargetsConfigAppliesLoginCustomerIDWithoutMutatingAppConfig(t *testing.T) {
	cfg := &config.Config{}
	adsConfig := googleads.Config{LoginCustomerID: "original"}

	got := newAdTargetsConfig(cfg, adsConfig, "override")

	assert.Equal(t, "original", adsConfig.LoginCustomerID)
	assert.Equal(t, "override", got.GoogleAds.LoginCustomerID)
	assert.Same(t, cfg, got.App)
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

func TestHydrateTargetsSupportsNonKrogerLocationIDs(t *testing.T) {
	lat := 47.62
	lon := -122.34
	targets, err := hydrateTargets(context.Background(), fakeLocations{
		locations: map[string]*locationtypes.Location{
			"wholefoods_10260": {ID: "wholefoods_10260", Name: "Whole Foods", Address: "2210 Westlake Ave", Lat: &lat, Lon: &lon},
		},
	}, []string{"wholefoods_10260"}, 2, "https://careme.cooking")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "wholefoods_10260", targets[0].StoreID)
	assert.Equal(t, "https://careme.cooking/recipes?location=wholefoods_10260", targets[0].FinalURL)
}

func TestRecipeURL(t *testing.T) {
	assert.Equal(t, "https://careme.cooking/recipes?location=70100023", recipeURL("https://careme.cooking", "70100023"))
}

func TestMissingProximityTargetsSkipsExistingShape(t *testing.T) {
	create, skip := missingProximityTargets([]googleads.Target{
		{StoreID: "1", LatMicro: 47610000, LonMicro: -122200000, RadiusMiles: 2},
		{StoreID: "2", LatMicro: 48610000, LonMicro: -123200000, RadiusMiles: 2},
	}, []googleads.ProximityCriterion{
		{LatMicro: 47610000, LonMicro: -122200000, RadiusMiles: 2},
	})

	assert.Equal(t, []googleads.Target{{StoreID: "2", LatMicro: 48610000, LonMicro: -123200000, RadiusMiles: 2}}, create)
	assert.Equal(t, []googleads.Target{{StoreID: "1", LatMicro: 47610000, LonMicro: -122200000, RadiusMiles: 2}}, skip)
}

func TestMissingAdGroupTargetsSkipsExistingName(t *testing.T) {
	targets := []googleads.Target{
		{StoreID: "1", StoreName: "Store One"},
		{StoreID: "2", StoreName: "Store Two"},
	}
	create, skip := missingAdGroupTargets(targets, []googleads.AdGroupSummary{
		{Name: googleads.AdGroupName(targets[0])},
	})

	assert.Equal(t, []googleads.Target{{StoreID: "2", StoreName: "Store Two"}}, create)
	assert.Equal(t, []googleads.Target{{StoreID: "1", StoreName: "Store One"}}, skip)
}

func TestPrintManualStepsIncludesStoreURLAndProximity(t *testing.T) {
	var out bytes.Buffer
	err := printManualSteps(&out, "5812848025", "23939758740", "PAUSED", []googleads.Target{{
		StoreID:     "70100023",
		StoreName:   "Bellevue Fred Meyer",
		LatMicro:    47610000,
		LonMicro:    -122200000,
		RadiusMiles: 2,
		FinalURL:    "https://careme.cooking/recipes?location=70100023",
	}})
	require.NoError(t, err)

	assert.Contains(t, out.String(), "https://careme.cooking/recipes?location=70100023")
	assert.Contains(t, out.String(), "47.610000, -122.200000, 2.00 miles")
	assert.Contains(t, out.String(), "Careme Store 70100023 Bellevue Fred Meyer")
	assert.Contains(t, out.String(), "Ad group level")
	assert.Contains(t, out.String(), "Ad level")
	assert.Contains(t, out.String(), `"healthy local recipes"`)
}

type fakeLocations struct {
	locations map[string]*locationtypes.Location
}

func (f fakeLocations) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	return f.locations[locationID], nil
}
