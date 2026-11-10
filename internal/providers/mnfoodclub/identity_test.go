package mnfoodclub

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMNFoodClubHashesWeekly(t *testing.T) {
	hashForStore := func(id string, date time.Time) []byte {
		return NewIdentityProvider().HashForStore(id, date).Sum(nil)
	}
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	// Include year boundaries and both daylight saving transitions.
	for _, monday := range []time.Time{
		time.Date(2026, 10, 19, 9, 0, 0, 0, zone),
		time.Date(2025, 12, 29, 9, 0, 0, 0, zone),
		time.Date(2026, 3, 2, 9, 0, 0, 0, zone),
		time.Date(2026, 10, 26, 9, 0, 0, 0, zone),
	} {
		for _, id := range []string{"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery"} {
			want := hashForStore(id, monday)
			for day := range 7 {
				date := monday.AddDate(0, 0, day)
				assert.Equal(t, want, hashForStore(id, date))
			}
			assert.Equal(t, want, hashForStore(id, monday.AddDate(0, 0, 7).Add(-time.Nanosecond)))
			assert.NotEqual(t, want, hashForStore(id, monday.AddDate(0, 0, 7)))
			assert.NotEqual(t, want, hashForStore(id, monday.Add(-time.Nanosecond)))
		}
		assert.NotEqual(t, hashForStore("mnfoodclub_delivery", monday), hashForStore("tcfarm_delivery", monday))
		assert.NotEqual(t, hashForStore("mnfoodclub_delivery", monday), hashForStore("nelsongrassfarm_delivery", monday))
		assert.NotEqual(t, hashForStore("tcfarm_delivery", monday), hashForStore("nelsongrassfarm_delivery", monday))
	}
}
