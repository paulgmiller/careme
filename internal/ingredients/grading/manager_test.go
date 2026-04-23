package grading

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGradeBackend struct {
	grades map[string]ai.InputIngredient
	err    error
	calls  [][]ai.InputIngredient
}

func (s *stubGradeBackend) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calls = append(s.calls, append([]ai.InputIngredient(nil), ingredients...))
	out := make([]ai.InputIngredient, len(ingredients))
	for i, ingredient := range ingredients {
		key := ingredientKey(ingredient)
		if gradedIngredient, ok := s.grades[key]; ok {
			out[i] = gradedIngredient
			continue
		}
		out[i] = ai.InputIngredient{
			ProductID:   ingredient.ProductID,
			Brand:       ingredient.Brand,
			Description: ingredient.Description,
			Size:        ingredient.Size,
			Categories:  slices.Clone(ingredient.Categories),
			Grade: &ai.IngredientGrade{
				SchemaVersion: "ingredient-grade-v1",
				Score:         10,
				Reason:        "default",
			},
		}
	}
	return out, nil
}

func TestPrioritizeIngredientsSortsAndFiltersLowScores(t *testing.T) {
	good := kroger.Ingredient{ProductId: strPtr("good-1"), Description: strPtr("Asparagus")}
	bad := kroger.Ingredient{ProductId: strPtr("bad-1"), Description: strPtr("Potato Chips")}
	goodInput, err := inputIngredientFromKrogerIngredient(good)
	require.NoError(t, err)
	badInput, err := inputIngredientFromKrogerIngredient(bad)
	require.NoError(t, err)
	backend := &stubGradeBackend{
		grades: map[string]ai.InputIngredient{
			ingredientKey(goodInput): {
				ProductID:   goodInput.ProductID,
				Description: goodInput.Description,
				Grade: &ai.IngredientGrade{
					SchemaVersion: "ingredient-grade-v1",
					Score:         9,
					Reason:        "Fresh vegetable.",
				},
			},
			ingredientKey(badInput): {
				ProductID:   badInput.ProductID,
				Description: badInput.Description,
				Grade: &ai.IngredientGrade{
					SchemaVersion: "ingredient-grade-v1",
					Score:         1,
					Reason:        "Snack food.",
				},
			},
		},
	}

	manager := &multiGrader{
		grader:    newCachingGrader(backend, NewStore(cache.NewInMemoryCache())),
		threshold: 3,
		minimum:   1,
	}

	got, err := manager.PrioritizeIngredients(t.Context(), []kroger.Ingredient{bad, good})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Asparagus", *got[0].Description)
}

func TestPrioritizeIngredientsFallsBackToOriginalWhenGradingFails(t *testing.T) {
	ingredient := kroger.Ingredient{ProductId: strPtr("chicken-1"), Description: strPtr("Chicken")}
	backend := &stubGradeBackend{err: errors.New("boom")}
	manager := &multiGrader{
		grader:    newCachingGrader(backend, NewStore(cache.NewInMemoryCache())),
		threshold: 3,
		minimum:   1,
	}

	got, err := manager.PrioritizeIngredients(t.Context(), []kroger.Ingredient{ingredient})
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
		id := strPtr(fmt.Sprintf("ingredient-%02d", i))
		ingredients[i] = kroger.Ingredient{ProductId: id, Description: name}
	}

	inputs := make([]ai.InputIngredient, len(ingredients))
	for i, ingredient := range ingredients {
		input, err := inputIngredientFromKrogerIngredient(ingredient)
		require.NoError(t, err)
		inputs[i] = input
	}

	results, err := grader.GradeIngredients(t.Context(), inputs)
	require.NoError(t, err)
	require.Len(t, results, 65)
	require.Len(t, backend.calls, 1)
	assert.Len(t, backend.calls[0], 65)
}

func TestMultiGraderBatchesUniqueIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	manager := &multiGrader{
		grader:    newCachingGrader(backend, cacheStore),
		threshold: 3,
		minimum:   1,
	}

	ingredients := make([]kroger.Ingredient, 65)
	for i := range ingredients {
		name := strPtr(fmt.Sprintf("Ingredient %02d", i))
		id := strPtr(fmt.Sprintf("ingredient-%02d", i))
		ingredients[i] = kroger.Ingredient{ProductId: id, Description: name}
	}

	results, err := manager.GradeIngredients(t.Context(), ingredients)
	require.NoError(t, err)
	require.Len(t, results, 65)
	require.Len(t, backend.calls, 3)
	callSizes := []int{len(backend.calls[0]), len(backend.calls[1]), len(backend.calls[2])}
	slices.Sort(callSizes)
	assert.Equal(t, []int{5, 30, 30}, callSizes)
}

func TestMultiGraderDedupesBeforeBatching(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	manager := &multiGrader{
		grader:    newCachingGrader(backend, cacheStore),
		threshold: 3,
		minimum:   1,
	}

	ingredients := make([]kroger.Ingredient, 0, 70)
	for i := 0; i < 35; i++ {
		name := strPtr(fmt.Sprintf("Ingredient %02d", i))
		id := strPtr(fmt.Sprintf("ingredient-%02d", i))
		ingredient := kroger.Ingredient{ProductId: id, Description: name}
		ingredients = append(ingredients, ingredient)
	}
	ingredients = append(ingredients, slices.Clone(ingredients)...)

	results, err := manager.GradeIngredients(t.Context(), ingredients)
	require.NoError(t, err)
	require.Len(t, results, 70)
	require.Len(t, backend.calls, 2)
	callSizes := []int{len(backend.calls[0]), len(backend.calls[1])}
	slices.Sort(callSizes)
	assert.Equal(t, []int{5, 30}, callSizes)
}

func TestGradeIngredientsRejectsBlankProductID(t *testing.T) {
	manager := &multiGrader{
		grader:    newCachingGrader(&stubGradeBackend{}, NewStore(cache.NewInMemoryCache())),
		threshold: 3,
		minimum:   1,
	}

	_, err := manager.GradeIngredients(t.Context(), []kroger.Ingredient{{Description: strPtr("Asparagus")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "product_id is required")
}

func strPtr(value string) *string {
	return &value
}
