package recipes

import (
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedProduceScorerUsesTodayCacheBeforeYesterday(t *testing.T) {
	c := cache.NewInMemoryCache()
	loc := testProduceScoreLocation()
	today := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.FixedZone("test", -5*60*60))
	yesterday := today.AddDate(0, 0, -1)
	seedProduceScoreIngredients(t, c, loc, yesterday, repeatGradedIngredients(10, 10))
	todayIngredients := append(repeatGradedIngredients(10, 11),
		gradedIngredient(6),
		ai.InputIngredient{},
	)
	seedProduceScoreIngredients(t, c, loc, today, todayIngredients)
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score := NewCachedProduceScorer(IO(c)).ProduceScore(t.Context(), *loc)

	require.NotNil(t, score)
	assert.Equal(t, 1, score.Score)
	assert.Equal(t, "2026-01-15", score.Date.Format("2006-01-02"))
}

func TestCachedProduceScorerFallsBackToYesterday(t *testing.T) {
	c := cache.NewInMemoryCache()
	loc := testProduceScoreLocation()
	yesterday := time.Date(2026, time.January, 14, 0, 0, 0, 0, time.FixedZone("test", -5*60*60))
	yesterdayIngredients := append(repeatGradedIngredients(9, 12), gradedIngredient(5))
	seedProduceScoreIngredients(t, c, loc, yesterday, yesterdayIngredients)
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score := NewCachedProduceScorer(IO(c)).ProduceScore(t.Context(), *loc)

	require.NotNil(t, score)
	assert.Equal(t, 1, score.Score)
	assert.Equal(t, "2026-01-14", score.Date.Format("2006-01-02"))
}

func TestCachedProduceScorerReturnsNilWhenCacheMissing(t *testing.T) {
	c := cache.NewInMemoryCache()
	loc := testProduceScoreLocation()
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score := NewCachedProduceScorer(IO(c)).ProduceScore(t.Context(), *loc)

	assert.Nil(t, score)
}

func TestSumIngredientGradesAboveCutoff(t *testing.T) {
	ingredients := append(repeatGradedIngredients(10, 10),
		gradedIngredient(7),
		gradedIngredient(6),
		gradedIngredient(0),
		ai.InputIngredient{},
	)

	assert.Equal(t, 1, sumIngredientGradesAboveCutoff(ingredients))
}

func testProduceScoreLocation() *locations.Location {
	return &locations.Location{
		ID:      "23456789",
		Name:    "Test Store",
		ZipCode: "10001",
	}
}

func gradedIngredient(score int) ai.InputIngredient {
	return ai.InputIngredient{
		Grade: &ai.IngredientGrade{
			Score: score,
		},
	}
}

func repeatGradedIngredients(score, count int) []ai.InputIngredient {
	ingredients := make([]ai.InputIngredient, 0, count)
	for range count {
		ingredients = append(ingredients, gradedIngredient(score))
	}
	return ingredients
}

func seedProduceScoreIngredients(t *testing.T, c cache.Cache, loc *locations.Location, date time.Time, ingredients []ai.InputIngredient) {
	t.Helper()
	params := DefaultParams(loc, date)
	require.NoError(t, IO(c).SaveIngredients(t.Context(), params.LocationHash(), ingredients))
}

func withNow(t *testing.T, now time.Time) {
	t.Helper()
	oldNowFn := nowFn
	nowFn = func() time.Time {
		return now
	}
	t.Cleanup(func() {
		nowFn = oldNowFn
	})
}
