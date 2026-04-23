package grading

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"
	"careme/internal/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGradeBackend struct {
	grades map[string]*ai.IngredientGrade
	err    error
}

func (s stubGradeBackend) GradeIngredient(_ context.Context, key string, ingredient kroger.Ingredient) (*ai.IngredientGrade, error) {
	if s.err != nil {
		return nil, s.err
	}
	if grade, ok := s.grades[key]; ok {
		return grade, nil
	}
	return &ai.IngredientGrade{
		SchemaVersion: "ingredient-grade-v1",
		Score:         10,
		Decision:      ai.IngredientDecisionKeep,
		Reason:        "default",
		Ingredient:    ai.SnapshotFromKrogerIngredient(ingredient),
	}, nil
}

func (s stubGradeBackend) Ready(context.Context) error { return nil }

func TestPrioritizeIngredientsSortsAndFiltersLowScores(t *testing.T) {
	good := kroger.Ingredient{Description: strPtr("Asparagus")}
	bad := kroger.Ingredient{Description: strPtr("Potato Chips")}
	locationHash := "loc-hash"

	manager := &multiGrader{
		grader: stubGradeBackend{
			grades: map[string]*ai.IngredientGrade{
				ingredientKey(locationHash, good): {
					SchemaVersion: "ingredient-grade-v1",
					Score:         9,
					Decision:      ai.IngredientDecisionKeep,
					Reason:        "Fresh vegetable.",
					Ingredient:    ai.SnapshotFromKrogerIngredient(good),
				},
				ingredientKey(locationHash, bad): {
					SchemaVersion: "ingredient-grade-v1",
					Score:         1,
					Decision:      ai.IngredientDecisionDrop,
					Reason:        "Snack food.",
					Ingredient:    ai.SnapshotFromKrogerIngredient(bad),
				},
			},
		},
		threshold: 3,
		minimum:   1,
	}

	got, err := manager.PrioritizeIngredients(t.Context(), locationHash, []kroger.Ingredient{bad, good})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Asparagus", *got[0].Description)
}

func TestPrioritizeIngredientsFallsBackToOriginalWhenGradingFails(t *testing.T) {
	ingredient := kroger.Ingredient{Description: strPtr("Chicken")}
	manager := &multiGrader{
		grader:    stubGradeBackend{err: errors.New("boom")},
		threshold: 3,
		minimum:   1,
	}

	got, err := manager.PrioritizeIngredients(t.Context(), "loc-hash", []kroger.Ingredient{ingredient})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Chicken", *got[0].Description)
}

func strPtr(value string) *string {
	return &value
}
