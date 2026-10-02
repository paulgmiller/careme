package cachekey

import (
	"testing"
	"time"

	"careme/internal/locations"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/mnfoodclub"

	"github.com/stretchr/testify/assert"
)

func TestStaplesSignatureForLocation_UsesAlbertsonsIdentityProvider(t *testing.T) {
	t.Parallel()

	got := locations.StaplesSignature("safeway_1142")
	want := albertsons.NewIdentityProvider().Signature()
	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestStaplesSignatureForLocation_UsesAldiIdentityProvider(t *testing.T) {
	t.Parallel()

	got := locations.StaplesSignature("aldi_F100")
	want := aldi.NewIdentityProvider().Signature()
	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestStaplesSignatureForLocation_PanicsForUnknownLocation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for unknown location")
		}
	}()

	_ = locations.StaplesSignature("loc-unknown")
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
			for _, hour := range []int{0, 12, 23} {
				date := time.Date(2025, 9, 17, hour, 0, 0, 0, time.FixedZone("store", -7*60*60))
				signature := locations.StaplesSignature(id)
				assert.Equal(t, want, ForStore(id, date, signature))
				assert.NotEqual(t, want, ForStore(id, date.AddDate(0, 0, 1), signature))
			}
		})
	}
}

func TestMNFoodClubSignature(t *testing.T) {
	assert.Equal(t, mnfoodclub.NewIdentityProvider().Signature(), locations.StaplesSignature("mnfoodclub_delivery"))
	date := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	signature := locations.StaplesSignature("mnfoodclub_delivery")
	assert.NotPanics(t, func() { _ = ForStore("mnfoodclub_delivery", date, signature) })
	assert.NotEqual(t, ForStore("mnfoodclub_delivery", date, signature), ForStore("mnfoodclub_other", date, signature))
	assert.NotEqual(t, ForStore("mnfoodclub_delivery", date, signature), ForStore("mnfoodclub_delivery", date.AddDate(0, 0, 1), signature))
}
