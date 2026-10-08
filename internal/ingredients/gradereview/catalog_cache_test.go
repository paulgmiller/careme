package gradereview

import (
	"context"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationLookup struct {
	location *locations.Location
	err      error
}

func (f fakeLocationLookup) GetLocationByID(context.Context, string) (*locations.Location, error) {
	return f.location, f.err
}

type failingIngredientCache struct {
	err   error
	calls int
}

func (f *failingIngredientCache) IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error) {
	f.calls++
	return nil, f.err
}

func TestCachedCatalogUsesStoreDayAndYesterday(t *testing.T) {
	lat, lon := 44.98, -93.26
	location := &locations.Location{ID: "70100023", Name: "Test store", Lat: &lat, Lon: &lon}
	// 8 am local time: the produce score still uses the previous store day.
	now := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	day, err := locations.StoreToDate(t.Context(), now, location)
	require.NoError(t, err)
	require.Equal(t, "2026-10-07", day.Format("2006-01-02"))
	today := []ai.InputIngredient{{ProductID: "today", Grade: &ai.IngredientGrade{Score: 9, Reason: "cached grade"}}, {ProductID: "ungraded"}}
	yesterday := []ai.InputIngredient{{ProductID: "yesterday", Grade: &ai.IngredientGrade{Score: 7, Reason: "previous day"}}}
	for _, tt := range []struct {
		name                     string
		seedToday, seedYesterday bool
		want                     []ai.InputIngredient
	}{
		{"today first", true, true, today}, {"yesterday", false, true, yesterday}, {"missing", false, false, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			rio := recipes.IO(c)
			if tt.seedToday {
				require.NoError(t, rio.SaveIngredients(t.Context(), cachekey.ForStore(location.ID, day, cachekey.StaplesSignature(location.ID)), today))
			}
			if tt.seedYesterday {
				require.NoError(t, rio.SaveIngredients(t.Context(), cachekey.ForStore(location.ID, day.AddDate(0, 0, -1), cachekey.StaplesSignature(location.ID)), yesterday))
			}
			catalog := NewCachedCatalog(fakeLocationLookup{location: location}, rio)
			catalog.now = func() time.Time { return now }
			gotLocation, ingredients, err := catalog.LoadCatalog(t.Context(), location.ID)
			if tt.want == nil {
				require.ErrorIs(t, err, cache.ErrNotFound)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, location, gotLocation)
			assert.Equal(t, tt.want, ingredients, "cached grades must be preserved without grading")
		})
	}
}

func TestCachedCatalogFailures(t *testing.T) {
	lat, lon := 44.98, -93.26
	location := &locations.Location{ID: "70100023", Lat: &lat, Lon: &lon}
	for _, tt := range []struct {
		name      string
		lookup    fakeLocationLookup
		cacheErr  error
		wantCalls int
	}{
		{"location", fakeLocationLookup{err: assert.AnError}, nil, 0},
		{"store date", fakeLocationLookup{location: &locations.Location{ID: "70100023"}}, nil, 0},
		{"corrupt cache", fakeLocationLookup{location: location}, assert.AnError, 1},
		{"canceled", fakeLocationLookup{location: location}, context.Canceled, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &failingIngredientCache{err: tt.cacheErr}
			catalog := NewCachedCatalog(tt.lookup, c)
			_, _, err := catalog.LoadCatalog(t.Context(), "70100023")
			require.Error(t, err)
			if tt.cacheErr != nil {
				require.ErrorIs(t, err, tt.cacheErr)
			}
			assert.Equal(t, tt.wantCalls, c.calls)
		})
	}
}
