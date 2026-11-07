package main

import (
	"context"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptimalStudySetBeatsGreedyCoverage(t *testing.T) {
	catalogs := []studyCatalog{
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "fish"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "pear"}, {ProductID: "beef"}}},
	}
	points := map[string]int{"apple": 10, "pear": 10, "fish": 9, "beef": 9}
	greedy := evaluateStudy("store", catalogs, points, 1)
	assert.Equal(t, []int{0}, greedy.Frontier[0].Searches)
	best, err := optimalStudySet(t.Context(), catalogs, points, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, best.Searches)
	assert.Equal(t, 38, best.GradePoints)
	assert.Equal(t, 4, best.Requests)
	// Strictly below four requests cannot use a pair.
	best, err = optimalStudySet(t.Context(), catalogs, points, 3)
	require.NoError(t, err)
	assert.Equal(t, 20, best.GradePoints)
	assert.Equal(t, 2, best.Requests)
}

func TestOptimalStudySetDoesNotLimitSearchCount(t *testing.T) {
	var catalogs []studyCatalog
	points := map[string]int{}
	for i := range 10 {
		id := string(rune('a' + i))
		catalogs = append(catalogs, studyCatalog{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: id}}})
		points[id] = 10
	}
	best, err := optimalStudySet(t.Context(), catalogs, points, 9)
	require.NoError(t, err)
	assert.Len(t, best.Searches, 9)
	assert.Equal(t, 9, best.Requests)
	assert.Equal(t, 90, best.GradePoints)
}

func TestOptimalStudySetBreaksTiesByRequestsThenSearches(t *testing.T) {
	catalogs := []studyCatalog{
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "apple"}}},
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "pear"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
		{Requests: 3, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
	}
	best, err := optimalStudySet(t.Context(), catalogs, map[string]int{"apple": 10, "pear": 9}, 3)
	require.NoError(t, err)
	assert.Equal(t, []int{2}, best.Searches)
	assert.Equal(t, 2, best.Requests)
	assert.Equal(t, 19, best.GradePoints)
}

func TestOptimalStudySetRejectsInvalidCostsAndCancellation(t *testing.T) {
	_, err := optimalStudySet(t.Context(), nil, nil, 0)
	require.ErrorContains(t, err, "budget must be positive")
	_, err = optimalStudySet(t.Context(), []studyCatalog{{Requests: 0}}, nil, 39)
	require.ErrorContains(t, err, "invalid request count")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = optimalStudySet(ctx, nil, nil, 39)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOptimalStudyFrontierDropsPlateaus(t *testing.T) {
	catalogs := []studyCatalog{
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "fish"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "pear"}, {ProductID: "beef"}}},
	}
	curve, err := optimalStudyFrontier(t.Context(), catalogs, map[string]int{"apple": 10, "pear": 10, "fish": 9, "beef": 9}, 6)
	require.NoError(t, err)
	require.Len(t, curve, 3)
	assert.Equal(t, 0, curve[0].Requests)
	assert.Equal(t, 20, curve[1].GradePoints)
	assert.Equal(t, 2, curve[1].Requests)
	assert.Equal(t, 38, curve[2].GradePoints)
	assert.Equal(t, 4, curve[2].Requests)
}

func TestOptimalStudyFrontierMatchesExhaustiveOracle(t *testing.T) {
	points := map[string]int{"a": 10, "b": 9, "c": 8, "d": 7, "e": 10, "f": 9, "g": 8}
	catalogs := []studyCatalog{
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "a"}, {ProductID: "b"}}},
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "a"}, {ProductID: "c"}, {ProductID: "d"}}},
		{Requests: 3, Ingredients: []ai.InputIngredient{{ProductID: "b"}, {ProductID: "e"}}},
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "f"}}},
		{Requests: 4, Ingredients: []ai.InputIngredient{{ProductID: "a"}, {ProductID: "b"}}}, // Dominated.
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "g"}, {ProductID: "f"}, {ProductID: "g"}}},
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "unhelpful"}}},
	}
	curve, err := optimalStudyFrontier(t.Context(), catalogs, points, 15)
	require.NoError(t, err)
	for budget := 1; budget <= 15; budget++ {
		var want searchSetScore
		for mask := 0; mask < 1<<len(catalogs); mask++ {
			var selected []int
			for i := range catalogs {
				if mask&(1<<i) != 0 {
					selected = append(selected, i)
				}
			}
			candidate := scoreSearchSet(catalogs, points, selected)
			if candidate.Requests > budget {
				continue
			}
			if candidate.GradePoints > want.GradePoints || (candidate.GradePoints == want.GradePoints && (candidate.Requests < want.Requests || (candidate.Requests == want.Requests && len(candidate.Searches) < len(want.Searches)))) {
				want = candidate
			}
		}
		var got searchSetScore
		for _, point := range curve {
			if point.Requests <= budget {
				got = point
			}
		}
		assert.Equal(t, want.GradePoints, got.GradePoints, "budget %d", budget)
		assert.Equal(t, want.Requests, got.Requests, "budget %d", budget)
		assert.Len(t, got.Searches, len(want.Searches), "budget %d", budget)
	}
}
