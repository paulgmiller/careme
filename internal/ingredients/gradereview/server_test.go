package gradereview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerRequiresLocation(t *testing.T) {
	h := NewHandler(cache.NewInMemoryCache(), fakeCatalog{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/grader", nil)
			if method == http.MethodPost {
				r = httptest.NewRequest(method, "/grader/review", strings.NewReader("grade_key=key&verdict=correct"))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Contains(t, rr.Body.String(), "location ID is required")
		})
	}
}

func TestHandlerSkipsUngradedIngredients(t *testing.T) {
	c := cache.NewInMemoryCache()
	graded := ai.InputIngredient{ProductID: "graded", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9, Reason: "Fresh"}}
	ungraded := ai.InputIngredient{ProductID: "ungraded", Description: "Ungraded item"}
	h := NewHandler(c, fakeCatalog{ingredients: map[string][]ai.InputIngredient{"a": {ungraded, graded}}})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Asparagus")
	assert.NotContains(t, rr.Body.String(), "Ungraded item")
	store := NewStore(c)
	require.NoError(t, store.Save(t.Context(), "a", store.catalogGradeKey(graded), []ai.InputIngredient{ungraded, graded}, VerdictCorrect, testReviewTime))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "No grades left to review")
	// An ungraded catalog item cannot be submitted as a review.
	require.ErrorIs(t, store.Save(t.Context(), "a", "catalog-v1/"+ungraded.Hash()+"/0", []ai.InputIngredient{ungraded}, VerdictCorrect, testReviewTime), cache.ErrNotFound)
}

func TestHandlerMissingCatalog(t *testing.T) {
	h := NewHandler(cache.NewInMemoryCache(), fakeCatalog{})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=missing", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "No cached ingredients found")
}

func TestHandlerRejectsInvalidReview(t *testing.T) {
	h := NewHandler(cache.NewInMemoryCache(), fakeCatalog{})
	r := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader("location=a&grade_key=key&verdict=great"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestChangedScoreCanBeReviewedAgain(t *testing.T) {
	s := NewStore(cache.NewInMemoryCache())
	ingredient := ai.InputIngredient{ProductID: "one", Grade: &ai.IngredientGrade{Score: 8, Reason: "Fresh"}}
	require.NoError(t, s.Save(t.Context(), "a", s.catalogGradeKey(ingredient), []ai.InputIngredient{ingredient}, VerdictCorrect, testReviewTime))
	ingredient.Grade = &ai.IngredientGrade{Score: 9, Reason: "Fresh"}
	candidate, err := s.Next(t.Context(), []ai.InputIngredient{ingredient})
	require.NoError(t, err)
	assert.Equal(t, ingredient, candidate.Ingredient)
}

type failingReviewCache struct{ cache.Cache }

func (f failingReviewCache) Exists(context.Context, string) (bool, error) {
	return false, assert.AnError
}

func (f failingReviewCache) Put(context.Context, string, string, cache.PutOptions) error {
	return assert.AnError
}

func TestHandlerReviewCacheFailure(t *testing.T) {
	ingredient := ai.InputIngredient{ProductID: "one", Grade: &ai.IngredientGrade{Score: 8, Reason: "Fresh"}}
	c := failingReviewCache{cache.NewInMemoryCache()}
	h := NewHandler(c, fakeCatalog{ingredients: map[string][]ai.InputIngredient{"a": {ingredient}}})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/grader?location=a", nil)
			if method == http.MethodPost {
				key := NewStore(c).catalogGradeKey(ingredient)
				r = httptest.NewRequest(method, "/grader/review", strings.NewReader("location=a&grade_key="+key+"&verdict=correct"))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			assert.Equal(t, http.StatusInternalServerError, rr.Code)
		})
	}
}

func TestReviewSnapshotExcludesEmbeddings(t *testing.T) {
	c := cache.NewInMemoryCache()
	s := NewStore(c)
	ingredient := ai.InputIngredient{ProductID: "one", Grade: &ai.IngredientGrade{Score: 8, Reason: "Fresh"}, Embedding: ai.IngredientEmbedding{1, 0}}
	key := s.catalogGradeKey(ingredient)
	require.NoError(t, s.Save(t.Context(), "a", key, []ai.InputIngredient{ingredient}, VerdictCorrect, testReviewTime))
	reader, err := c.Get(t.Context(), reviewCachePrefix+key)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()
	var review Review
	require.NoError(t, json.NewDecoder(reader).Decode(&review))
	assert.Empty(t, review.Ingredient.Embedding)
	assert.NotEmpty(t, ingredient.Embedding)
	assert.Equal(t, testReviewTime, review.ReviewedAt)
}
