package gradereview

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testReviewTime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func newTestStore(c cache.Cache, items map[string][]ai.InputIngredient) *store {
	store := NewStore(c, fakeLocationLookup{err: cache.ErrNotFound}, &failingIngredientCache{err: assert.AnError})
	for id, ingredients := range items {
		store.entries[id] = &catalogEntry{location: &locations.Location{ID: id, Name: "Store " + id}, ingredients: slices.Clone(ingredients)}
	}
	return store
}

func TestStoreReviewFlow(t *testing.T) {
	c := cache.NewInMemoryCache()
	ingredient := ai.InputIngredient{ProductID: "one", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9, Reason: "Fresh."}}
	other := ai.InputIngredient{ProductID: "two", Description: "Prepared dip", Grade: &ai.IngredientGrade{Score: 2, Reason: "Prepared."}}
	h := NewHandler(newTestStore(c, map[string][]ai.InputIngredient{"a": {ingredient}, "b": {other}}))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Store a")
	assert.Contains(t, response.Body.String(), "Asparagus")
	assert.NotContains(t, response.Body.String(), "Prepared dip")
	assert.Contains(t, response.Body.String(), `name="location" value="a"`)
	key := catalogGradeKey(ingredient)
	post := func(location, gradeKey, verdict string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader(url.Values{"location": {location}, "grade_key": {gradeKey}, "verdict": {verdict}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		return response
	}
	// Posting a valid grade for another store must not create a review.
	require.Equal(t, http.StatusNotFound, post("b", key, "correct").Code)
	exists, err := c.Exists(t.Context(), reviewCachePrefix+key)
	require.NoError(t, err)
	assert.False(t, exists)
	response = post("a", key, "too_high")
	require.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/grader?location=a", response.Header().Get("Location"))
	reader, err := c.Get(t.Context(), reviewCachePrefix+key)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var review reviewRecord
	require.NoError(t, json.Unmarshal(body, &review))
	assert.NotContains(t, string(body), "location_id")
	assert.Equal(t, ingredient, review.Ingredient)
	assert.Equal(t, verdictTooHigh, review.Verdict)
	require.Equal(t, http.StatusNotFound, post("a", key, "too_low").Code)
	savedReader, err := c.Get(t.Context(), reviewCachePrefix+key)
	require.NoError(t, err)
	defer func() { require.NoError(t, savedReader.Close()) }()
	var saved reviewRecord
	require.NoError(t, json.NewDecoder(savedReader).Decode(&saved))
	assert.Equal(t, verdictTooHigh, saved.Verdict, "first review wins")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "No grades left to review, chef")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=b", nil))
	assert.Contains(t, response.Body.String(), "Prepared dip")
}

func TestStoreReviewLoadingFailure(t *testing.T) {
	h := NewHandler(NewStore(cache.NewInMemoryCache(), fakeLocationLookup{err: errors.New("catalog unavailable")}, &failingIngredientCache{err: assert.AnError}))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/grader?location=a", nil)
			if method == http.MethodPost {
				r = httptest.NewRequest(method, "/grader/review", strings.NewReader("location=a&grade_key=version/key&verdict=correct"))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, r)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}

func TestSharedIngredientReviewSkippedAcrossStores(t *testing.T) {
	c := cache.NewInMemoryCache()
	ingredient := ai.InputIngredient{ProductID: "shared", Grade: &ai.IngredientGrade{Score: 8, Reason: "Flexible"}}
	s := newTestStore(c, map[string][]ai.InputIngredient{"a": {ingredient}, "b": {ingredient}})
	require.NoError(t, s.save(t.Context(), "a", catalogGradeKey(ingredient), verdictCorrect, testReviewTime))
	_, candidate, err := s.Next(t.Context(), "b")
	require.NoError(t, err)
	assert.Empty(t, candidate.GradeKey)
}
