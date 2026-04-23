package grading

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGradeBackend struct {
	grades map[string]*ai.IngredientGrade
	err    error
	calls  [][]kroger.Ingredient
}

func (s *stubGradeBackend) GradeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]*ai.IngredientGrade, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls = append(s.calls, append([]kroger.Ingredient(nil), ingredients...))
	out := make([]*ai.IngredientGrade, len(ingredients))
	for i, ingredient := range ingredients {
		key := ingredientKey("loc-hash", ingredient)
		if grade, ok := s.grades[key]; ok {
			out[i] = grade
			continue
		}
		out[i] = &ai.IngredientGrade{
			SchemaVersion: "ingredient-grade-v1",
			Score:         10,
			Reason:        "default",
			Ingredient:    ai.SnapshotFromKrogerIngredient(ingredient),
		}
	}
	return out, nil
}

func (s *stubGradeBackend) Ready(context.Context) error { return nil }

func TestPrioritizeIngredientsSortsAndFiltersLowScores(t *testing.T) {
	good := kroger.Ingredient{Description: strPtr("Asparagus")}
	bad := kroger.Ingredient{Description: strPtr("Potato Chips")}
	locationHash := "loc-hash"
	backend := &stubGradeBackend{
		grades: map[string]*ai.IngredientGrade{
			ingredientKey(locationHash, good): {
				SchemaVersion: "ingredient-grade-v1",
				Score:         9,
				Reason:        "Fresh vegetable.",
				Ingredient:    ai.SnapshotFromKrogerIngredient(good),
			},
			ingredientKey(locationHash, bad): {
				SchemaVersion: "ingredient-grade-v1",
				Score:         1,
				Reason:        "Snack food.",
				Ingredient:    ai.SnapshotFromKrogerIngredient(bad),
			},
		},
	}

	manager := &multiGrader{
		grader:    newCachingGrader(backend, NewStore(cache.NewInMemoryCache())),
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
	backend := &stubGradeBackend{err: errors.New("boom")}
	manager := &multiGrader{
		grader:    newCachingGrader(backend, NewStore(cache.NewInMemoryCache())),
		threshold: 3,
		minimum:   1,
	}

	got, err := manager.PrioritizeIngredients(t.Context(), "loc-hash", []kroger.Ingredient{ingredient})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Chicken", *got[0].Description)
}

func TestCachingGraderBatchesMissingIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore)

	ingredients := make([]kroger.Ingredient, 65)
	for i := range ingredients {
		name := strPtr(fmt.Sprintf("Ingredient %02d", i))
		ingredients[i] = kroger.Ingredient{Description: name}
	}

	results, err := grader.GradeIngredients(t.Context(), "loc-hash", ingredients)
	require.NoError(t, err)
	require.Len(t, results, 65)
	require.Len(t, backend.calls, 3)
	assert.Len(t, backend.calls[0], 30)
	assert.Len(t, backend.calls[1], 30)
	assert.Len(t, backend.calls[2], 5)
}

func strPtr(value string) *string {
	return &value
}
