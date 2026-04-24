package grading

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIngredientGradeCacheVersion = "test-cache-version"

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
		key := ai.NormalizeInputIngredient(ingredient).Hash()
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
				Score:  10,
				Reason: "default",
			},
		}
	}
	return out, nil
}

func TestCachingGraderBatchesMissingIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore, testIngredientGradeCacheVersion)

	ingredients := make([]kroger.Ingredient, 65)
	for i := range ingredients {
		name := strPtr(fmt.Sprintf("Ingredient %02d", i))
		id := strPtr(fmt.Sprintf("ingredient-%02d", i))
		ingredients[i] = kroger.Ingredient{ProductId: id, Description: name}
	}

	inputs := make([]ai.InputIngredient, len(ingredients))
	for i, ingredient := range ingredients {
		input, err := InputIngredientFromKrogerIngredient(ingredient)
		require.NoError(t, err)
		inputs[i] = input
	}

	results, err := grader.GradeIngredients(t.Context(), inputs)
	require.NoError(t, err)
	require.Len(t, results, 65)
	require.Len(t, backend.calls, 1)
	assert.Len(t, backend.calls[0], 65)
}

func TestCachingGraderSkipsIngredientsThatAlreadyHaveGrades(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore, testIngredientGradeCacheVersion)

	preGraded := ai.InputIngredient{
		ProductID:   "ingredient-00",
		Description: "Ingredient 00",
		Grade: &ai.IngredientGrade{
			Score:  9,
			Reason: "already graded",
		},
	}
	ungraded := ai.InputIngredient{
		ProductID:   "ingredient-01",
		Description: "Ingredient 01",
	}

	results, err := grader.GradeIngredients(t.Context(), []ai.InputIngredient{preGraded, ungraded})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Len(t, backend.calls, 1)
	assert.Equal(t, []ai.InputIngredient{ungraded}, backend.calls[0])
	require.NotNil(t, results[0].Grade)
	assert.Equal(t, 9, results[0].Grade.Score)
	require.NotNil(t, results[1].Grade)
}

func TestMultiGraderBatchesUniqueIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	manager := &multiGrader{
		cacheVersion: testIngredientGradeCacheVersion,
		grader:       newCachingGrader(backend, cacheStore, testIngredientGradeCacheVersion),
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
		cacheVersion: testIngredientGradeCacheVersion,
		grader:       newCachingGrader(backend, cacheStore, testIngredientGradeCacheVersion),
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
		cacheVersion: testIngredientGradeCacheVersion,
		grader:       newCachingGrader(&stubGradeBackend{}, NewStore(cache.NewInMemoryCache()), testIngredientGradeCacheVersion),
	}

	_, err := manager.GradeIngredients(t.Context(), []kroger.Ingredient{{Description: strPtr("Asparagus")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "product_id is required")
}

func strPtr(value string) *string {
	return &value
}
