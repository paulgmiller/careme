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
	"careme/internal/providers/kroger"
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
		{"70500874", kroger.NewIdentityProvider()},
		{"safeway_1142", albertsons.NewIdentityProvider()},
		{"aldi_F100", aldi.NewIdentityProvider()},
		{"mnfoodclub_delivery", mnfoodclub.NewIdentityProvider()},
		{"tcfarm_delivery", mnfoodclub.NewIdentityProvider()},
		{"nelsongrassfarm_delivery", mnfoodclub.NewIdentityProvider()},
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
		"safeway_1142": "tCtsYBvO05c", "heb_540": "CMClGf16aHw",
		"aldi_F100": "2XFP-Eq4418", "publix_1847": "1ZjBLr7Mego", "farmersmarket_1": "g7OOZpxYEww",
		"wholefoods_10216": "3NnJs9GPTj4", "walmart_1": "WiU9sDZCk_E",
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

func TestKrogerCatalogIdentityRefreshesSnapshotsWithoutItemUPCs(t *testing.T) {
	date := time.Date(2025, 9, 17, 0, 0, 0, 0, time.UTC)
	assert.NotEqual(t, "y91ErIgebKw", cachekey.ForStore("70500874", date))
	assert.NotEqual(t, "wrxx3dmHzBA", cachekey.ForStore("loc-123", date))
}
