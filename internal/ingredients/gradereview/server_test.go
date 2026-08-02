package gradereview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/grading"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerReviewsIngredientAndAdvances(t *testing.T) {
	cacheStore := &countingCache{InMemoryCache: cache.NewInMemoryCache()}
	saveGrade(t, cacheStore, "version/one", ai.InputIngredient{
		ProductID:   "one",
		Brand:       "Garden Farm",
		Description: "Asparagus",
		Size:        "1 bunch",
		Categories:  []string{"produce", "vegetables"},
		Grade:       &ai.IngredientGrade{Score: 9, Reason: "Fresh and flexible."},
	})
	saveGrade(t, cacheStore, "version/two", ai.InputIngredient{
		ProductID:   "two",
		Description: "Prepared dip",
		Grade:       &ai.IngredientGrade{Score: 2, Reason: "Ready to eat."},
	})

	handler := NewHandler(cacheStore)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Asparagus")
	assert.Contains(t, response.Body.String(), "9<small>/10</small>")
	assert.Contains(t, response.Body.String(), "0 of 2 reviewed")
	assert.Contains(t, response.Body.String(), "Too high")
	assert.Contains(t, response.Body.String(), "Correct")
	assert.Contains(t, response.Body.String(), "Too low")

	form := url.Values{
		"grade_key": {"version/one"},
		"verdict":   {string(VerdictTooHigh)},
	}
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/", response.Header().Get("Location"))

	reviewReader, err := cacheStore.Get(t.Context(), reviewCachePrefix+"version/one")
	require.NoError(t, err)
	defer func() { require.NoError(t, reviewReader.Close()) }()
	var review Review
	require.NoError(t, json.NewDecoder(reviewReader).Decode(&review))
	assert.Equal(t, VerdictTooHigh, review.Verdict)
	assert.Equal(t, "Asparagus", review.Ingredient.Description)
	assert.False(t, review.ReviewedAt.IsZero())

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Prepared dip")
	assert.Contains(t, response.Body.String(), "1 of 2 reviewed")
	assert.Equal(t, 2, cacheStore.listCalls, "the grade and review indexes should each be listed only once")
}

func TestHandlerShowsCompletionWhenEveryGradeIsReviewed(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	saveGrade(t, cacheStore, "version/one", ai.InputIngredient{
		ProductID:   "one",
		Description: "Asparagus",
		Grade:       &ai.IngredientGrade{Score: 9, Reason: "Fresh and flexible."},
	})
	store := NewStore(cacheStore)
	require.NoError(t, store.Save(t.Context(), "version/one", VerdictCorrect, time.Now()))

	response := httptest.NewRecorder()
	NewHandler(cacheStore).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "All caught up")
	assert.Contains(t, response.Body.String(), "1 of 1 reviewed")
}

func TestHandlerRejectsInvalidReview(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	form := url.Values{
		"grade_key": {"version/one"},
		"verdict":   {"great"},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	NewHandler(cacheStore).ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func saveGrade(t *testing.T, cacheStore cache.Cache, key string, ingredient ai.InputIngredient) {
	t.Helper()
	body, err := json.Marshal(ingredient)
	require.NoError(t, err)
	require.NoError(t, cacheStore.Put(t.Context(), grading.CachePrefix()+key, string(body), cache.Unconditional()))
}

type countingCache struct {
	*cache.InMemoryCache
	listCalls int
}

func (c *countingCache) List(ctx context.Context, prefix string, token string) ([]string, error) {
	c.listCalls++
	return c.InMemoryCache.List(ctx, prefix, token)
}
