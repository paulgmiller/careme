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
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerRequiresLocation(t *testing.T) {
	h := NewHandler(newTestStore(cache.NewInMemoryCache(), nil))
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
	h := NewHandler(newTestStore(c, map[string][]ai.InputIngredient{"a": {ungraded, graded}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Asparagus")
	assert.NotContains(t, rr.Body.String(), "Ungraded item")
	store := newTestStore(c, map[string][]ai.InputIngredient{"a": {ungraded, graded}})
	require.NoError(t, store.Save(t.Context(), "a", catalogGradeKey(graded), VerdictCorrect, testReviewTime))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "No grades left to review")
	// An ungraded catalog item cannot be submitted as a review.
	require.ErrorIs(t, store.Save(t.Context(), "a", "catalog-v1/"+ungraded.Hash()+"/0", VerdictCorrect, testReviewTime), cache.ErrNotFound)
}

func TestHandlerMissingCatalog(t *testing.T) {
	h := NewHandler(newTestStore(cache.NewInMemoryCache(), nil))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/grader?location=missing", nil))
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "No cached ingredients found")
}

func TestHandlerRejectsInvalidReview(t *testing.T) {
	h := NewHandler(newTestStore(cache.NewInMemoryCache(), nil))
	r := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader("location=a&grade_key=key&verdict=great"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestChangedScoreCanBeReviewedAgain(t *testing.T) {
	ingredient := ai.InputIngredient{ProductID: "one", Grade: &ai.IngredientGrade{Score: 8, Reason: "Fresh"}}
	s := newTestStore(cache.NewInMemoryCache(), map[string][]ai.InputIngredient{"a": {ingredient}})
	require.NoError(t, s.Save(t.Context(), "a", catalogGradeKey(ingredient), VerdictCorrect, testReviewTime))
	ingredient.Grade = &ai.IngredientGrade{Score: 9, Reason: "Fresh"}
	s.entries["b"] = &catalogEntry{location: &locations.Location{ID: "b"}, ingredients: []ai.InputIngredient{ingredient}}
	candidate, err := s.Next(t.Context(), "b")
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
	h := NewHandler(newTestStore(c, map[string][]ai.InputIngredient{"a": {ingredient}}))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/grader?location=a", nil)
			if method == http.MethodPost {
				key := catalogGradeKey(ingredient)
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
	ingredient := ai.InputIngredient{ProductID: "one", Grade: &ai.IngredientGrade{Score: 8, Reason: "Fresh"}, Embedding: ai.IngredientEmbedding{1, 0}}
	s := newTestStore(c, map[string][]ai.InputIngredient{"a": {ingredient}})
	key := catalogGradeKey(ingredient)
	require.NoError(t, s.Save(t.Context(), "a", key, VerdictCorrect, testReviewTime))
	reader, err := c.Get(t.Context(), reviewCachePrefix+key)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()
	var review Review
	require.NoError(t, json.NewDecoder(reader).Decode(&review))
	assert.Empty(t, review.Ingredient.Embedding)
	assert.NotEmpty(t, ingredient.Embedding)
	assert.Equal(t, testReviewTime, review.ReviewedAt)
}
