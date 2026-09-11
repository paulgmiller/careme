package locations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultRecipeDate_Uses9AMStoreBoundary(t *testing.T) {
	storeLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	beforeBoundary := time.Date(2026, 1, 15, 13, 59, 0, 0, time.UTC) // 08:59 in New York
	before := defaultRecipeDate(beforeBoundary, storeLoc)
	if got, want := before.Format("2006-01-02"), "2026-01-14"; got != want {
		t.Fatalf("expected previous day before 9AM boundary, got %s", got)
	}

	atBoundary := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC) // 09:00 in New York
	after := defaultRecipeDate(atBoundary, storeLoc)
	if got, want := after.Format("2006-01-02"), "2026-01-15"; got != want {
		t.Fatalf("expected same day at 9AM boundary, got %s", got)
	}
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
