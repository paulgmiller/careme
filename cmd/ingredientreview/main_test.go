package main

import (
	"context"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationLookup struct{ err error }

func (f fakeLocationLookup) GetLocationByID(context.Context, string) (*locations.Location, error) {
	if f.err != nil {
		return nil, f.err
	}
	lat, lon := 44.98, -93.26
	return &locations.Location{ID: "70100023", Name: "Test store", Lat: &lat, Lon: &lon}, nil
}

type fakeStaplesBackend struct {
	calls int
	err   error
}

func (*fakeStaplesBackend) IsID(id string) bool { return id == "70100023" }
func (*fakeStaplesBackend) Signature() string   { return "test" }
func (f *fakeStaplesBackend) FetchStaples(context.Context, string) ([]ai.InputIngredient, error) {
	f.calls++
	return []ai.InputIngredient{{ProductID: "one", Description: "Asparagus"}}, f.err
}

func (*fakeStaplesBackend) FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error) {
	return []ai.InputIngredient{}, nil
}

type fakeGrader struct{ inputs []ai.InputIngredient }

func (f *fakeGrader) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	f.inputs = append([]ai.InputIngredient(nil), ingredients...)
	out := append([]ai.InputIngredient(nil), ingredients...)
	for i := range out {
		out[i].Grade = &ai.IngredientGrade{Score: 9, Reason: "Fresh"}
	}
	return out, nil
}

func TestStoreCatalogFetchesAndCaches(t *testing.T) {
	backend := &fakeStaplesBackend{}
	grader := &fakeGrader{}
	c := cache.NewInMemoryCache()
	catalog := storeCatalog{locations: fakeLocationLookup{}, staples: recipes.NewCachedStaplesService([]recipes.StaplesBackend{backend}, c, reviewGrader{grader}), now: func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }}
	for range 2 {
		location, ingredients, err := catalog.LoadCatalog(t.Context(), "70100023")
		require.NoError(t, err)
		assert.Equal(t, "Test store", location.Name)
		require.Len(t, ingredients, 1)
		assert.Equal(t, 9, ingredients[0].Grade.Score)
		require.Len(t, grader.inputs, 1)
		assert.Nil(t, grader.inputs[0].Grade, "cached catalog grades must be cleared before configured grader")
	}
	assert.Equal(t, 1, backend.calls)
}

func TestStoreCatalogFailures(t *testing.T) {
	for _, stage := range []string{"location", "catalog"} {
		t.Run(stage, func(t *testing.T) {
			lookup := fakeLocationLookup{}
			backend := &fakeStaplesBackend{}
			if stage == "location" {
				lookup.err = assert.AnError
			} else {
				backend.err = assert.AnError
			}
			catalog := storeCatalog{locations: lookup, staples: recipes.NewCachedStaplesService([]recipes.StaplesBackend{backend}, cache.NewInMemoryCache(), reviewGrader{&fakeGrader{}}), now: time.Now}
			_, _, err := catalog.LoadCatalog(t.Context(), "70100023")
			require.ErrorIs(t, err, assert.AnError)
		})
	}
}
