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

func TestStoreDayPreservesZeroTimeForLegacyDates(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	date := time.Date(2026, 10, 21, 0, 0, 0, 0, zone)
	for _, tt := range []struct {
		name        string
		input, want time.Time
	}{
		{"date only", date, date},
		{"nanoseconds", date.Add(time.Nanosecond), date.AddDate(0, 0, -1)},
		{"seconds", date.Add(time.Second), date.AddDate(0, 0, -1)},
		{"minutes", date.Add(time.Minute), date.AddDate(0, 0, -1)},
		{"hours", date.Add(time.Hour), date.AddDate(0, 0, -1)},
		{"before boundary", date.Add(9*time.Hour - time.Nanosecond), date.AddDate(0, 0, -1)},
		{"at boundary", date.Add(9 * time.Hour), date},
		{"after boundary", date.Add(12 * time.Hour), date},
	} {
		t.Run(tt.name, func(t *testing.T) {
			day := StoreDay(tt.input)
			assert.Equal(t, tt.want, day)
			assert.Equal(t, day, StoreDay(day), "a resolved store day must not shift again")
			assert.Equal(t, base64.RawURLEncoding.EncodeToString(NewHash("loc-123", tt.want, "test").Sum(nil)), ForStore("loc-123", tt.input))
		})
	}
}
