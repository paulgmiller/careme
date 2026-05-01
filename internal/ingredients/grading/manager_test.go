package grading

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIngredientGradeCacheVersion = "test-cache-version"

type stubGradeBackend struct {
	calls [][]ai.InputIngredient
}

func (s *stubGradeBackend) CacheVersion() string {
	return testIngredientGradeCacheVersion
}

func (s *stubGradeBackend) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	s.calls = append(s.calls, append([]ai.InputIngredient(nil), ingredients...))
	var out []ai.InputIngredient
	for _, ingredient := range ingredients {
		ingredient.Grade = &ai.IngredientGrade{
			Score:  10,
			Reason: "default",
		}
		// this should be closer to whats in actual grader.
		out = append(out, ingredient)
	}
	return out, nil
}

func TestCachingGraderBatchesMissingIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore)

	inputs := make([]ai.InputIngredient, 65)
	for i := range inputs {
		inputs[i] = ai.InputIngredient{
			ProductID:   fmt.Sprintf("ingredient-%02d", i),
			Description: fmt.Sprintf("Ingredient %02d", i),
		}
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
	grader := newCachingGrader(backend, cacheStore)

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

func TestCachingGraderOverlaysCachedGradeOnCurrentIngredientMetadata(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore)

	current := ai.InputIngredient{
		ProductID:    "ingredient-00",
		AisleNumber:  "fresh-vegetables",
		Brand:        "Whole Foods Market",
		Description:  "Organic Asparagus",
		Size:         "1 lb",
		PriceRegular: new(float32(5.99)),
		PriceSale:    new(float32(4.99)),
		Categories:   []string{"Produce"},
	}
	cached := ai.InputIngredient{
		ProductID:   current.ProductID,
		Brand:       current.Brand,
		Description: current.Description,
		Size:        current.Size,
		Grade: &ai.IngredientGrade{
			Score:  9,
			Reason: "cached grade",
		},
	}
	key := cacheKey(testIngredientGradeCacheVersion + "/" + ingredientHash(current))
	require.NoError(t, cacheStore.Save(t.Context(), key, &cached))

	results, err := grader.GradeIngredients(t.Context(), []ai.InputIngredient{current})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Empty(t, backend.calls)
	assert.Equal(t, "fresh-vegetables", results[0].AisleNumber)
	assert.Equal(t, []string{"Produce"}, results[0].Categories)
	require.NotNil(t, results[0].PriceRegular)
	require.NotNil(t, results[0].PriceSale)
	assert.Equal(t, float32(5.99), *results[0].PriceRegular)
	assert.Equal(t, float32(4.99), *results[0].PriceSale)
	require.NotNil(t, results[0].Grade)
	assert.Equal(t, 9, results[0].Grade.Score)
	assert.Equal(t, "cached grade", results[0].Grade.Reason)
}

func TestCachingGraderOverlaysNewGradeOnCurrentIngredientMetadata(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	grader := newCachingGrader(backend, cacheStore)

	current := ai.InputIngredient{
		ProductID:   "ingredient-00",
		AisleNumber: "fresh-herbs",
		Description: "Organic Basil",
		Categories:  []string{"Produce"},
	}

	results, err := grader.GradeIngredients(t.Context(), []ai.InputIngredient{current})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fresh-herbs", results[0].AisleNumber)
	assert.Equal(t, []string{"Produce"}, results[0].Categories)
	require.NotNil(t, results[0].Grade)
	assert.Equal(t, 10, results[0].Grade.Score)

	cached, err := cacheStore.Load(t.Context(), cacheKey(testIngredientGradeCacheVersion+"/"+ingredientHash(current)))
	require.NoError(t, err)
	assert.Equal(t, "fresh-herbs", cached.AisleNumber)
	assert.Equal(t, []string{"Produce"}, cached.Categories)
	require.NotNil(t, cached.Grade)
}

func TestMultiGraderBatchesUniqueIngredientsInChunksOf30(t *testing.T) {
	cacheStore := NewStore(cache.NewInMemoryCache())
	backend := &stubGradeBackend{}
	manager := newCachingGrader(&multiGrader{backend}, cacheStore)

	ingredients := make([]ai.InputIngredient, 65)
	for i := range ingredients {
		ingredients[i] = ai.InputIngredient{
			ProductID:   fmt.Sprintf("ingredient-%02d", i),
			Description: fmt.Sprintf("Ingredient %02d", i),
		}
	}

	results, err := manager.GradeIngredients(t.Context(), ingredients)
	require.NoError(t, err)
	require.Len(t, results, 65)
	require.Len(t, backend.calls, 3)
	callSizes := []int{len(backend.calls[0]), len(backend.calls[1]), len(backend.calls[2])}
	slices.Sort(callSizes)
	assert.Equal(t, []int{5, 30, 30}, callSizes)
}
