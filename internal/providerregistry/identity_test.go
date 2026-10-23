package providerregistry_test

import (
	"encoding/base64"
	"testing"
	"time"

	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
	_ "careme/internal/providerregistry"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
)

func TestRegisteredProvidersOwnHashPolicy(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 10, 21, 14, 35, 27, 123, time.FixedZone("store", -5*60*60))
	for _, tt := range []struct {
		id       string
		provider cachekey.StoreHasher
	}{
		{"safeway_1142", albertsons.NewIdentityProvider()},
		{"aldi_F100", aldi.NewIdentityProvider()},
		{"mnfoodclub_delivery", mnfoodclub.NewIdentityProvider()},
		{"tcfarm_delivery", mnfoodclub.NewIdentityProvider()},
		{"nelsongrassfarm_delivery", mnfoodclub.NewIdentityProvider()},
		{"mnfoodclub_other", mnfoodclub.NewIdentityProvider()},
		{"smithbrothersfarms_delivery", smithbrothersfarms.NewIdentityProvider()},
	} {
		t.Run(tt.id, func(t *testing.T) {
			want := base64.RawURLEncoding.EncodeToString(tt.provider.HashForStore(tt.id, date).Sum(nil))
			assert.Equal(t, want, cachekey.ForStore(tt.id, date))
			params := recipes.DefaultParams(&locations.Location{ID: tt.id}, date)
			assert.Equal(t, date, params.Date)
			assert.Equal(t, want, params.Hash())
			assert.Equal(t, want, params.LocationHash())
		})
	}
	assert.Panics(t, func() { cachekey.ForStore("loc-unknown", date) })
}

func TestForStorePreservesExistingHashes(t *testing.T) {
	// Captured from generatorParams.LocationHash before extraction.
	hashes := map[string]string{
		"70500874": "y91ErIgebKw", "safeway_1142": "tCtsYBvO05c", "heb_540": "CMClGf16aHw",
		"aldi_F100": "2XFP-Eq4418", "publix_1847": "1ZjBLr7Mego", "farmersmarket_1": "g7OOZpxYEww",
		"wholefoods_10216": "3NnJs9GPTj4", "walmart_1": "WiU9sDZCk_E", "loc-123": "wrxx3dmHzBA",
	}
	for id, want := range hashes {
		t.Run(id, func(t *testing.T) {
			legacy := &recipes.GeneratorParams{Location: &locations.Location{ID: id}, Date: time.Date(2025, 9, 17, 0, 0, 0, 0, time.UTC)}
			assert.Equal(t, want, legacy.Hash())
			assert.Equal(t, want, legacy.LocationHash())
			for _, hour := range []int{0, 9, 12, 23} {
				date := time.Date(2025, 9, 17, hour, 0, 0, 0, time.FixedZone("store", -7*60*60))
				assert.Equal(t, want, cachekey.ForStore(id, date))
				params := recipes.DefaultParams(&locations.Location{ID: id}, date)
				assert.Equal(t, want, params.LocationHash())
				assert.Equal(t, want, params.Hash())
				assert.NotEqual(t, want, cachekey.ForStore(id, date.AddDate(0, 0, 1)))
			}
		})
	}
}

func TestMNFoodClubHashesWeekly(t *testing.T) {
	zone, err := time.LoadLocation("America/Chicago")
	assert.NoError(t, err)
	// Include year boundaries and both daylight saving transitions.
	for _, monday := range []time.Time{
		time.Date(2026, 10, 19, 9, 0, 0, 0, zone),
		time.Date(2025, 12, 29, 9, 0, 0, 0, zone),
		time.Date(2026, 3, 2, 9, 0, 0, 0, zone),
		time.Date(2026, 10, 26, 9, 0, 0, 0, zone),
	} {
		want := cachekey.ForStore("mnfoodclub_delivery", monday)
		for _, id := range []string{"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery"} {
			for day := range 7 {
				date := monday.AddDate(0, 0, day)
				assert.Equal(t, want, cachekey.ForStore(id, date))
				params := recipes.DefaultParams(&locations.Location{ID: id}, date)
				assert.Equal(t, want, params.Hash())
				assert.Equal(t, want, params.LocationHash())
			}
			assert.Equal(t, want, cachekey.ForStore(id, monday.AddDate(0, 0, 7).Add(-time.Nanosecond)))
			assert.NotEqual(t, want, cachekey.ForStore(id, monday.AddDate(0, 0, 7)))
			assert.NotEqual(t, want, cachekey.ForStore(id, monday.Add(-time.Nanosecond)))
		}
		assert.NotEqual(t, want, cachekey.ForStore("mnfoodclub_other", monday))
	}
}

func TestSmithBrothersFarmsHashesDaily(t *testing.T) {
	date := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	assert.NotEqual(t, cachekey.ForStore("smithbrothersfarms_delivery", date), cachekey.ForStore("smithbrothersfarms_other", date))
	assert.NotEqual(t, cachekey.ForStore("smithbrothersfarms_delivery", date), cachekey.ForStore("smithbrothersfarms_delivery", date.AddDate(0, 0, 1)))
}
