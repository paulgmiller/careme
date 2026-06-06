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
	seedProduceScoreIngredients(t, c, loc, yesterday, []ai.InputIngredient{
		gradedIngredient(10),
	})
	seedProduceScoreIngredients(t, c, loc, today, []ai.InputIngredient{
		gradedIngredient(8),
		gradedIngredient(7),
		gradedIngredient(6),
		{},
	})
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score, err := NewCachedProduceScorer(c).ProduceScore(t.Context(), loc)

	require.NoError(t, err)
	require.NotNil(t, score)
	assert.Equal(t, 15, score.Score)
	assert.Equal(t, "2026-01-15", score.Date.Format("2006-01-02"))
}

func TestCachedProduceScorerFallsBackToYesterday(t *testing.T) {
	c := cache.NewInMemoryCache()
	loc := testProduceScoreLocation()
	yesterday := time.Date(2026, time.January, 14, 0, 0, 0, 0, time.FixedZone("test", -5*60*60))
	seedProduceScoreIngredients(t, c, loc, yesterday, []ai.InputIngredient{
		gradedIngredient(9),
		gradedIngredient(5),
	})
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score, err := NewCachedProduceScorer(c).ProduceScore(t.Context(), loc)

	require.NoError(t, err)
	require.NotNil(t, score)
	assert.Equal(t, 9, score.Score)
	assert.Equal(t, "2026-01-14", score.Date.Format("2006-01-02"))
}

func TestCachedProduceScorerReturnsNilWhenCacheMissing(t *testing.T) {
	c := cache.NewInMemoryCache()
	loc := testProduceScoreLocation()
	withNow(t, time.Date(2026, time.January, 15, 15, 0, 0, 0, time.UTC))

	score, err := NewCachedProduceScorer(c).ProduceScore(t.Context(), loc)

	require.NoError(t, err)
	assert.Nil(t, score)
}

func TestSumIngredientGradesAboveCutoff(t *testing.T) {
	ingredients := []ai.InputIngredient{
		gradedIngredient(10),
		gradedIngredient(7),
		gradedIngredient(6),
		gradedIngredient(0),
		{},
	}

	assert.Equal(t, 17, sumIngredientGradesAboveCutoff(ingredients))
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
