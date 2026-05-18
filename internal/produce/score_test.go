package produce

import (
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreIngredientsRewardsVarietyOverDuplicateVariants(t *testing.T) {
	t.Parallel()

	carrotVariants := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 8),
		ingredient("carrot-2", "Baby Carrots", 8),
		ingredient("carrot-3", "Rainbow Carrots", 8),
		ingredient("carrot-4", "Organic Carrots", 8),
	})
	rootVariety := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 8),
		ingredient("beet-1", "Fresh Beets", 8),
		ingredient("parsnip-1", "Fresh Parsnips", 8),
	})

	require.Equal(t, 1, carrotVariants.MatchedFamilies)
	require.Equal(t, 3, rootVariety.MatchedFamilies)
	assert.Greater(t, rootVariety.Score, carrotVariants.Score)
}

func TestScoreIngredientsUsesGradesWithinFamily(t *testing.T) {
	t.Parallel()

	low := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 4),
	})
	high := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 9),
	})

	assert.Greater(t, high.Score, low.Score)
	assert.Equal(t, 9, high.TopMatchedFamilies[0].BestGrade)
}

func TestScoreIngredientsAppliesDiminishingReturnsAndFamilyCap(t *testing.T) {
	t.Parallel()

	oneCarrot := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 5),
	})
	twoCarrots := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 5),
		ingredient("carrot-2", "Baby Carrots", 5),
	})
	carrotAndBeet := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 5),
		ingredient("beet-1", "Fresh Beets", 5),
	})
	manyCarrots := ScoreIngredients([]ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 10),
		ingredient("carrot-2", "Baby Carrots", 10),
		ingredient("carrot-3", "Rainbow Carrots", 10),
	})

	assert.Greater(t, twoCarrots.Score, oneCarrot.Score)
	assert.Greater(t, carrotAndBeet.Score, twoCarrots.Score)
	assert.InDelta(t, 10.0, manyCarrots.TopMatchedFamilies[0].Points, 0.001)
}

func TestScoreIngredientsDoesNotInflateScoreForUngradedMatches(t *testing.T) {
	t.Parallel()

	score := ScoreIngredients([]ai.InputIngredient{
		{ProductID: "carrot-1", Description: "Fresh Carrots"},
	})

	assert.Equal(t, 1, score.MatchedFamilies)
	assert.Equal(t, 0, score.GradedCount)
	assert.Equal(t, 1, score.UngradedCount)
	assert.Zero(t, score.Score)
}

func TestMatchFamiliesPrefersSpecificNestedProduceTerms(t *testing.T) {
	t.Parallel()

	families := MatchFamilies("Fresh Green Onions")

	assert.Contains(t, families, "green onion")
	assert.NotContains(t, families, "onion")
}

func ingredient(id, description string, grade int) ai.InputIngredient {
	return ai.InputIngredient{
		ProductID:   id,
		Description: description,
		Grade: &ai.IngredientGrade{
			Score:  grade,
			Reason: "test",
		},
	}
}
