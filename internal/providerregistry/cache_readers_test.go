package providerregistry_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/gradereview"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/producescore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cacheReaderLocation struct{ location *locations.Location }

func (l cacheReaderLocation) GetLocationByID(context.Context, string) (*locations.Location, error) {
	return l.location, nil
}

func TestCacheReadersFindWeeklyRecipeIngredients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lat, lon := 45.152, -93.194
		location := &locations.Location{ID: "mnfoodclub_delivery", Lat: &lat, Lon: &lon}
		// The synthetic clock starts on Saturday UTC (Friday evening in Chicago).
		localNow, err := locations.StoreTime(t.Context(), time.Now(), location)
		require.NoError(t, err)
		require.Equal(t, time.Friday, localNow.Weekday())
		monday := localNow.AddDate(0, 0, -4)
		monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 9, 0, 0, 0, monday.Location())
		params := recipes.DefaultParams(location, monday)
		ingredients := []ai.InputIngredient{{ProductID: "weekly", Description: "Weekly produce share", Grade: &ai.IngredientGrade{Score: 10, Reason: "Fresh produce"}}}
		c := cache.NewInMemoryCache()
		rio := recipes.IO(c)
		require.NoError(t, rio.SaveIngredients(t.Context(), params.LocationHash(), ingredients))

		score := producescore.NewCachedProduceScorer(rio).ProduceScore(t.Context(), *location)
		require.NotNil(t, score, "Friday scoring must find Monday's snapshot")
		assert.Equal(t, 0, *score)

		reviews := gradereview.NewStore(c, cacheReaderLocation{location: location}, rio)
		gotLocation, candidate, err := reviews.Next(t.Context(), location.ID)
		require.NoError(t, err)
		assert.Equal(t, location, gotLocation)
		require.NotNil(t, candidate)
		assert.Equal(t, ingredients[0], candidate.Ingredient)
	})
}
