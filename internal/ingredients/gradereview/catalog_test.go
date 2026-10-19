package gradereview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCatalog struct {
	ingredients map[string][]ai.InputIngredient
	err         error
}

func (f fakeCatalog) LoadCatalog(_ context.Context, id string) (*locations.Location, []ai.InputIngredient, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	ingredients, ok := f.ingredients[id]
	if !ok {
		return nil, nil, cache.ErrNotFound
	}
	return &locations.Location{ID: id, Name: "Store " + id}, ingredients, nil
}

func TestStoreReviewFlow(t *testing.T) {
	c := cache.NewInMemoryCache()
	ingredient := ai.InputIngredient{ProductID: "one", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9, Reason: "Fresh."}}
	other := ai.InputIngredient{ProductID: "two", Description: "Prepared dip", Grade: &ai.IngredientGrade{Score: 2, Reason: "Prepared."}}
	h := NewHandler(c, fakeCatalog{ingredients: map[string][]ai.InputIngredient{"a": {ingredient}, "b": {other}}})
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Store a")
	assert.Contains(t, response.Body.String(), "Asparagus")
	assert.NotContains(t, response.Body.String(), "Prepared dip")
	assert.Contains(t, response.Body.String(), `name="location" value="a"`)
	key := NewStore(c).catalogGradeKey(ingredient)
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
	var review Review
	require.NoError(t, json.NewDecoder(reader).Decode(&review))
	assert.Equal(t, "a", review.LocationID)
	assert.Equal(t, ingredient, review.Ingredient)
	assert.Equal(t, VerdictTooHigh, review.Verdict)
	require.Equal(t, http.StatusSeeOther, post("a", key, "too_low").Code)
	saved, err := readReview(t.Context(), c, reviewCachePrefix+key)
	require.NoError(t, err)
	assert.Equal(t, VerdictTooHigh, saved.Verdict, "first review wins")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "No grades left to review, chef")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=b", nil))
	assert.Contains(t, response.Body.String(), "Prepared dip")
}

func TestStoreReviewLoadingFailure(t *testing.T) {
	h := NewHandler(cache.NewInMemoryCache(), fakeCatalog{err: errors.New("catalog unavailable")})
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
	s := NewStore(c)
	ingredient := ai.InputIngredient{ProductID: "shared", Grade: &ai.IngredientGrade{Score: 8, Reason: "Flexible"}}
	require.NoError(t, s.SaveFromCatalog(t.Context(), "a", s.catalogGradeKey(ingredient), []ai.InputIngredient{ingredient}, VerdictCorrect, testReviewTime))
	candidate, err := s.NextFromCatalog(t.Context(), []ai.InputIngredient{ingredient})
	require.NoError(t, err)
	assert.Empty(t, candidate.GradeKey)
}
