package main

import (
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDedupeIngredientsBySlugUsesBestGrade(t *testing.T) {
	ingredients := []ai.InputIngredient{
		testIngredient("baby-carrots", "Baby Carrots", "carrots", 7),
		testIngredient("rainbow-carrots", "Rainbow Carrots", "carrots", 9),
		testIngredient("baby-bok-choy", "Baby Bok Choy", "baby bok choy", 8),
		testIngredient("bok-choy", "Bok Choy", "bok choy", 6),
	}

	deduped := dedupeIngredientsBySlug(ingredients)
	require.Len(t, deduped, 3)

	bySlug := make(map[string]ai.InputIngredient, len(deduped))
	for _, ingredient := range deduped {
		bySlug[ingredient.Slug] = ingredient
	}

	assert.Equal(t, "rainbow-carrots", bySlug["carrots"].ProductID)
	assert.Equal(t, "baby-bok-choy", bySlug["baby bok choy"].ProductID)
	assert.Equal(t, "bok-choy", bySlug["bok choy"].ProductID)

	summary := summarizeGrades(deduped)
	assert.Equal(t, 3, summary.TotalCount)
	assert.Equal(t, 23, summary.ScoreSum)
}

func TestDedupeIngredientsBySlugFallsBackToNormalizedDescription(t *testing.T) {
	ingredients := []ai.InputIngredient{
		testIngredient("lower", " asparagus ", "", 6),
		testIngredient("upper", "ASPARAGUS", "", 8),
	}

	deduped := dedupeIngredientsBySlug(ingredients)
	require.Len(t, deduped, 1)
	assert.Equal(t, "upper", deduped[0].ProductID)
}

func TestSummarizeGradesSkipsUngradedIngredients(t *testing.T) {
	summary := summarizeGrades([]ai.InputIngredient{
		testIngredient("graded", "Asparagus", "asparagus", 8),
		{ProductID: "ungraded", Description: "Mystery ingredient"},
	})

	assert.Equal(t, 1, summary.TotalCount)
	assert.Equal(t, 8, summary.ScoreSum)
	assert.Equal(t, 1, summary.Counts[8])
}

func TestGroupUsefulIngredientsBySlugIncludesAllIngredientsForUsefulSlug(t *testing.T) {
	ingredients := []ai.InputIngredient{
		testIngredient("rainbow-carrots", "Rainbow Carrots", "carrots", 9),
		testIngredient("baby-carrots", "Baby Carrots", "carrots", 5),
		testIngredient("potato-chips", "Potato Chips", "potato chips", 2),
		testIngredient("bok-choy", "Bok Choy", "bok choy", 7),
	}

	groups := groupUsefulIngredientsBySlug(ingredients)
	require.Len(t, groups, 2)

	assert.Equal(t, "carrots", groups[0].Slug)
	assert.Equal(t, 9, groups[0].BestGrade)
	require.Len(t, groups[0].Ingredients, 2)
	assert.Equal(t, "rainbow-carrots", groups[0].Ingredients[0].ProductID)
	assert.Equal(t, "baby-carrots", groups[0].Ingredients[1].ProductID)

	assert.Equal(t, "bok choy", groups[1].Slug)
	assert.Equal(t, 7, groups[1].BestGrade)
	require.Len(t, groups[1].Ingredients, 1)
}

func TestGroupUsefulIngredientsBySlugSkipsGroupsWithoutGradeAboveSix(t *testing.T) {
	groups := groupUsefulIngredientsBySlug([]ai.InputIngredient{
		testIngredient("baby-carrots", "Baby Carrots", "carrots", 6),
		testIngredient("rainbow-carrots", "Rainbow Carrots", "carrots", 4),
		{ProductID: "mystery", Description: "Mystery Ingredient", Slug: "mystery ingredient"},
	})

	assert.Empty(t, groups)
}

func testIngredient(id, description, slug string, score int) ai.InputIngredient {
	return ai.InputIngredient{
		ProductID:   id,
		Description: description,
		Slug:        slug,
		Grade: &ai.IngredientGrade{
			Score:  score,
			Reason: "test",
		},
	}
}
