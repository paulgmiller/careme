package smithbrothersfarms

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmithBrothersFarmsHashesWeekly(t *testing.T) {
	hashForStore := func(id string, date time.Time) []byte {
		return NewIdentityProvider().HashForStore(id, date).Sum(nil)
	}
	zone, err := time.LoadLocation("America/Los_Angeles")
	require.NoError(t, err)
	for _, monday := range []time.Time{
		time.Date(2026, 10, 19, 9, 0, 0, 0, zone),
		time.Date(2025, 12, 29, 9, 0, 0, 0, zone),
		time.Date(2026, 3, 2, 9, 0, 0, 0, zone),
		time.Date(2026, 10, 26, 9, 0, 0, 0, zone),
	} {
		want := hashForStore("smithbrothersfarms_delivery", monday)
		for day := range 7 {
			date := monday.AddDate(0, 0, day)
			assert.Equal(t, want, hashForStore("smithbrothersfarms_delivery", date))
		}
		assert.Equal(t, want, hashForStore("smithbrothersfarms_delivery", monday.AddDate(0, 0, 7).Add(-time.Nanosecond)))
		assert.NotEqual(t, want, hashForStore("smithbrothersfarms_delivery", monday.AddDate(0, 0, 7)))
		assert.NotEqual(t, want, hashForStore("smithbrothersfarms_delivery", monday.Add(-time.Nanosecond)))
		assert.NotEqual(t, want, hashForStore("smithbrothersfarms_other", monday))
	}
}
