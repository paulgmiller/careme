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
	"careme/internal/locations/geo"
	"careme/internal/providers/fullcircle"
	"careme/internal/recipes"
	"careme/internal/recipes/producescore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cacheReaderLocation struct{ location *locations.Location }

func (l cacheReaderLocation) GetLocationByID(context.Context, string) (*locations.Location, error) {
	return l.location, nil
}

func TestProduceScoreFindsFullCircleDeliveryIngredients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := fullcircle.NewLocationBackend()
		location, err := backend.GetLocationByID(t.Context(), "fullcircle_delivery")
		require.NoError(t, err)
		localNow, err := locations.StoreTime(t.Context(), time.Now(), location)
		require.NoError(t, err)
		rio := recipes.IO(cache.NewInMemoryCache())
		ingredients := make([]ai.InputIngredient, 20)
		for i := range ingredients {
			ingredients[i] = ai.InputIngredient{Grade: &ai.IngredientGrade{Score: 10}}
		}
		require.NoError(t, rio.SaveIngredients(t.Context(), recipes.DefaultParams(location, localNow).LocationHash(), ingredients))
		for _, point := range []geo.Coordinate{
			{Lat: 47.6062, Lon: -122.3321}, // Seattle
			{Lat: 47.6101, Lon: -122.2015}, // Bellevue
			{Lat: 47.5301, Lon: -122.0326}, // Issaquah
		} {
			found, err := backend.GetLocationsByCoordinates(t.Context(), point)
			require.NoError(t, err)
			require.Len(t, found, 1)
			score := producescore.NewCachedProduceScorer(rio).ProduceScore(t.Context(), found[0])
			require.NotNil(t, score)
			assert.Equal(t, 2, *score)
		}
	})
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
