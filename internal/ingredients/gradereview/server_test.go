package gradereview

import (
	"context"
	"encoding/json/v2"
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
	saveGrade(t, cacheStore, "version/ve/one", ai.InputIngredient{
		ProductID:   "one",
		Brand:       "Garden Farm",
		Description: "Asparagus",
		Size:        "1 bunch",
		Categories:  []string{"produce", "vegetables"},
		Grade:       &ai.IngredientGrade{Score: 9, Reason: "Fresh and flexible."},
	})
	saveGrade(t, cacheStore, "version/ve/two", ai.InputIngredient{
		ProductID:   "two",
		Description: "Prepared dip",
		Grade:       &ai.IngredientGrade{Score: 2, Reason: "Ready to eat."},
	})

	store := NewStore(cacheStore, "version")
	store.prefix = func() (string, error) { return "ve", nil }
	handler := newHandler(store)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader", nil))

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Asparagus")
	assert.Contains(t, response.Body.String(), "9<small>/10</small>")
	assert.Contains(t, response.Body.String(), "Too high")
	assert.Contains(t, response.Body.String(), "Correct")
	assert.Contains(t, response.Body.String(), "Too low")
	assert.Equal(t, []string{grading.CachePrefix() + "version/ve"}, cacheStore.listPrefixes)

	form := url.Values{
		"grade_key": {"version/ve/one"},
		"verdict":   {string(VerdictTooHigh)},
	}
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/grader", response.Header().Get("Location"))

	reviewReader, err := cacheStore.Get(t.Context(), reviewCachePrefix+"version/ve/one")
	require.NoError(t, err)
	defer func() { require.NoError(t, reviewReader.Close()) }()
	var review Review
	require.NoError(t, json.UnmarshalRead(reviewReader, &review))
	assert.Equal(t, VerdictTooHigh, review.Verdict)
	assert.Equal(t, "Asparagus", review.Ingredient.Description)
	assert.False(t, review.ReviewedAt.IsZero())

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Prepared dip")
	assert.Equal(t, 1, cacheStore.listCalls, "the sampled grade index should only be listed once")
}

func TestHandlerShowsCompletionWhenEveryGradeIsReviewed(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	saveGrade(t, cacheStore, "version/ve/one", ai.InputIngredient{
		ProductID:   "one",
		Description: "Asparagus",
		Grade:       &ai.IngredientGrade{Score: 9, Reason: "Fresh and flexible."},
	})
	reviewer := NewStore(cacheStore, "version")
	reviewer.prefix = func() (string, error) { return "ve", nil }
	require.NoError(t, reviewer.Save(t.Context(), "version/ve/one", VerdictCorrect, time.Now()))
	store := NewStore(cacheStore, "version")
	store.prefix = func() (string, error) { return "ve", nil }

	response := httptest.NewRecorder()
	newHandler(store).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader", nil))

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "No grades found")
}

func TestStoreReloadsWithANewPrefixWhenBatchIsFullyReviewed(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	saveGrade(t, cacheStore, "version/ab/one", ai.InputIngredient{
		ProductID: "one",
		Grade:     &ai.IngredientGrade{Score: 9, Reason: "Flexible."},
	})
	saveGrade(t, cacheStore, "version/cd/two", ai.InputIngredient{
		ProductID: "two",
		Grade:     &ai.IngredientGrade{Score: 3, Reason: "Prepared."},
	})

	prefixes := []string{"ab", "cd"}
	store := NewStore(cacheStore, "version")
	store.prefix = func() (string, error) {
		prefix := prefixes[0]
		prefixes = prefixes[1:]
		return prefix, nil
	}
	require.NoError(t, store.Save(t.Context(), "version/ab/one", VerdictCorrect, time.Now()))

	candidate, err := store.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "version/cd/two", candidate.GradeKey)
	assert.Equal(t, "two", candidate.Ingredient.ProductID)
}

func TestHandlerRejectsInvalidReview(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	form := url.Values{
		"grade_key": {"version/one"},
		"verdict":   {"great"},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	NewHandler(cacheStore, "version").ServeHTTP(response, request)

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
	listCalls    int
	listPrefixes []string
}

func (c *countingCache) List(ctx context.Context, prefix string, token string) ([]string, error) {
	c.listCalls++
	c.listPrefixes = append(c.listPrefixes, prefix)
	return c.InMemoryCache.List(ctx, prefix, token)
}
