package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	"careme/internal/providerregistry"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInventory bool

func (f fakeInventory) HasInventory(string) bool { return bool(f) }

type fakeScoreStaples struct {
	params      *recipes.GeneratorParams
	ingredients []ai.InputIngredient
	err         error
}

func (f *fakeScoreStaples) FetchStaples(_ context.Context, params *recipes.GeneratorParams) ([]ai.InputIngredient, error) {
	f.params = params
	return f.ingredients, f.err
}

func TestScoreLocations(t *testing.T) {
	fetchError := errors.New("fetch staples failed")
	for _, test := range []struct {
		name      string
		supported bool
		err       error
	}{
		{name: "scores fetched ingredients", supported: true},
		{name: "skips unsupported stores"},
		{name: "reports fetch failure", supported: true, err: fetchError},
	} {
		t.Run(test.name, func(t *testing.T) {
			ingredients := []ai.InputIngredient{{Grade: &ai.IngredientGrade{Score: 6}}, {}}
			for range 12 {
				ingredients = append(ingredients, ai.InputIngredient{Grade: &ai.IngredientGrade{Score: 10}})
			}
			staples := &fakeScoreStaples{ingredients: ingredients, err: test.err}
			signatures := providerregistry.SignatureFactory{}
			scorer := scoreService{inventory: fakeInventory(test.supported), staples: staples, signatures: signatures}
			locs := []locations.Location{
				{ID: "70500874", ZipCode: "98101", Lat: new(47.61), Lon: new(-122.33)},
				{ID: "70500010", ZipCode: "98101", Lat: new(47.61), Lon: new(-122.33)},
			}

			rows, err := scorer.scoreLocations(t.Context(), locs, 1)
			if test.supported {
				require.NotNil(t, staples.params)
				assert.Equal(t, locs[0].ID, staples.params.Location.ID)
				assert.Equal(t, signatures.StaplesSignature(locs[0].ID), staples.params.StaplesSignature)
			} else {
				assert.Nil(t, staples.params)
			}
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
				return
			}
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, test.supported, rows[0].SupportsStaples)
			if test.supported {
				assert.Equal(t, len(ingredients), rows[0].IngredientCount)
				require.NotNil(t, rows[0].ProduceScore)
				assert.Equal(t, 1, *rows[0].ProduceScore)
			} else {
				assert.Nil(t, rows[0].ProduceScore)
			}
		})
	}
}

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
