package cachekey

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashWithoutRegistry(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 10, 21, 12, 34, 56, 789, time.UTC)
	for _, id := range []string{"loc-123", "another-store"} {
		assert.Equal(t, base64.RawURLEncoding.EncodeToString(NewHash(id, date, "test").Sum(nil)), ForStore(id, date))
	}
}

func TestStoreDayUsesLocal9AMBoundary(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	for _, date := range []time.Time{
		time.Date(2026, 10, 21, 0, 0, 0, 0, zone),
		time.Date(2026, 3, 8, 0, 0, 0, 0, zone),
		time.Date(2026, 11, 1, 0, 0, 0, 0, zone),
		time.Date(2026, 1, 1, 0, 0, 0, 0, zone),
	} {
		boundary := time.Date(date.Year(), date.Month(), date.Day(), 9, 0, 0, 0, zone)
		assert.Equal(t, date.AddDate(0, 0, -1), StoreDay(boundary.Add(-time.Nanosecond)))
		assert.Equal(t, date, StoreDay(boundary))
		assert.Equal(t, date, StoreDay(boundary.Add(12*time.Hour)))
		assert.Equal(t, ForStore("loc-123", boundary.AddDate(0, 0, -1)), ForStore("loc-123", boundary.Add(-time.Nanosecond)))
		assert.NotEqual(t, ForStore("loc-123", boundary), ForStore("loc-123", boundary.Add(-time.Nanosecond)))
	}
}

func TestLegacyHashDoesNotApplyCutoff(t *testing.T) {
	date := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, NewHash("loc-123", date, "test").Sum(nil), HashForLegacyStoreDate("loc-123", date).Sum(nil))
	assert.NotEqual(t, HashForLegacyStoreDate("loc-123", date).Sum(nil), HashForStore("loc-123", date).Sum(nil))
}
