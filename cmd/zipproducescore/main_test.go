package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"careme/internal/locations"
	"careme/internal/locations/geo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationLookup struct {
	mu           sync.Mutex
	requestedIDs []string
}

func (f *fakeLocationLookup) GetLocationByID(_ context.Context, locationID string) (*locations.Location, error) {
	f.mu.Lock()
	f.requestedIDs = append(f.requestedIDs, locationID)
	f.mu.Unlock()
	lat := 47.61
	lon := -122.33
	return &locations.Location{
		ID:      locationID,
		Name:    "Hydrated " + locationID,
		ZipCode: "98101",
		Lat:     &lat,
		Lon:     &lon,
	}, nil
}

func (*fakeLocationLookup) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locations.Location, error) {
	return nil, fmt.Errorf("coordinate search should not be called")
}

func TestLocationsToScoreHydratesStaplesWatchdogLocationIDs(t *testing.T) {
	lookup := &fakeLocationLookup{}

	got, err := locationsToScore(t.Context(), lookup, "fake", true)

	require.NoError(t, err)
	require.Len(t, got, 8)
	gotIDs := make([]string, 0, len(got))
	for _, location := range got {
		gotIDs = append(gotIDs, location.ID)
		assert.NotEmpty(t, location.Name)
		assert.NotNil(t, location.Lat)
		assert.NotNil(t, location.Lon)
	}
	assert.ElementsMatch(t, gotIDs, lookup.requestedIDs)
}
