package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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

func TestPrintRows(t *testing.T) {
	score, zero := 42, 0
	location := locations.Location{ID: "70500874", Chain: "Kroger", Name: "Test Store", ZipCode: "98101"}
	rows := []scoreRow{
		{Location: location, SupportsStaples: true, IngredientCount: 123, ProduceScore: &score},
		{Location: location, SupportsStaples: true, ProduceScore: &zero},
		{Location: location, SupportsStaples: true},
		{Location: location},
	}
	var out bytes.Buffer
	printRows(&out, rows)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 5)
	headers := []string{"ID", "CHAIN", "NAME", "ZIP", "INGREDIENTS", "PRODUCE_SCORE", "STATUS"}
	require.Equal(t, headers, strings.Fields(lines[0]))
	starts := make([]int, len(headers))
	for i, header := range headers {
		starts[i] = strings.Index(lines[0], header)
	}
	expected := [][]string{
		{"70500874", "Kroger", "Test Store", "98101", "123", "42", "ok"},
		{"70500874", "Kroger", "Test Store", "98101", "0", "0", "ok"},
		{"70500874", "Kroger", "Test Store", "98101", "0", "", "score unavailable"},
		{"70500874", "Kroger", "Test Store", "98101", "0", "", "unsupported"},
	}
	for i, line := range lines[1:] {
		for j, start := range starts {
			end := len(line)
			if j+1 < len(starts) {
				end = starts[j+1]
			}
			require.GreaterOrEqual(t, len(line), end)
			assert.Equal(t, expected[i][j], strings.TrimSpace(line[start:end]), "row %d column %s", i, headers[j])
		}
	}
}
