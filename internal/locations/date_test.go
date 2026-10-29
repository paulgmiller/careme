package locations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreTimePreservesInstant(t *testing.T) {
	t.Parallel()
	lat, lon := 40.7128, -74.006
	location := &Location{ID: "store-1", Lat: &lat, Lon: &lon}
	for _, input := range []string{"2026-01-15T13:59:00Z", "2026-01-15T14:00:00Z", "2026-03-08T07:30:00Z", "2026-11-01T06:30:00Z"} {
		now, err := time.Parse(time.RFC3339, input)
		require.NoError(t, err)
		now = now.Add(123 * time.Nanosecond)
		local, err := StoreTime(t.Context(), now, location)
		require.NoError(t, err)
		require.True(t, now.Equal(local))
		require.Equal(t, "America/New_York", local.Location().String())
	}
}

func TestStoreTimeRejectsMissingLocation(t *testing.T) {
	_, err := StoreTime(t.Context(), time.Now(), nil)
	require.ErrorContains(t, err, "nil location")
}

func TestResolveStoreTimeLocation_RejectsLocationWithoutCoordinates(t *testing.T) {
	location := &Location{
		ID:      "store-1",
		Name:    "Test Store",
		ZipCode: "10001",
	}

	_, err := resolveStoreTimeLocation(t.Context(), location)

	require.EqualError(t, err, "location store-1 has no coordinates")
}
