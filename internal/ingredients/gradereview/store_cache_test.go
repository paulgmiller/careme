package gradereview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationLookup struct {
	location *locations.Location
	err      error
}

func (f fakeLocationLookup) GetLocationByID(context.Context, string) (*locations.Location, error) {
	return f.location, f.err
}

type failingIngredientCache struct {
	err   error
	calls int
}

func (f *failingIngredientCache) IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error) {
	f.calls++
	return nil, f.err
}

func loadTestCatalog(t *testing.T, store *store, id string) (*locations.Location, []ai.InputIngredient, error) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, err := store.loadCatalog(t.Context(), id)
	if err != nil {
		return nil, nil, err
	}
	return entry.location, slices.Clone(entry.ingredients), nil
}

func TestCachedCatalogUsesStoreDayAndYesterday(t *testing.T) {
	lat, lon := 44.98, -93.26
	location := &locations.Location{ID: "70100023", Name: "Test store", Lat: &lat, Lon: &lon}
	// 8 am local time: the produce score still uses the previous store day.
	now := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	day, err := locations.StoreTime(t.Context(), now, location)
	require.NoError(t, err)
	require.Equal(t, "2026-10-07", cachekey.StoreDay(day).Format("2006-01-02"))
	today := []ai.InputIngredient{{ProductID: "today", Grade: &ai.IngredientGrade{Score: 9, Reason: "cached grade"}}, {ProductID: "ungraded"}}
	yesterday := []ai.InputIngredient{{ProductID: "yesterday", Grade: &ai.IngredientGrade{Score: 7, Reason: "previous day"}}}
	for _, tt := range []struct {
		name                     string
		seedToday, seedYesterday bool
		want                     []ai.InputIngredient
	}{
		{"today first", true, true, today}, {"yesterday", false, true, yesterday}, {"missing", false, false, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			rio := recipes.IO(c)
			if tt.seedToday {
				require.NoError(t, rio.SaveIngredients(t.Context(), cachekey.ForStore(location.ID, day), today))
			}
			if tt.seedYesterday {
				require.NoError(t, rio.SaveIngredients(t.Context(), cachekey.ForStore(location.ID, day.AddDate(0, 0, -1)), yesterday))
			}
			catalog := NewStore(cache.NewInMemoryCache(), fakeLocationLookup{location: location}, rio)
			catalog.now = func() time.Time { return now }
			gotLocation, ingredients, err := loadTestCatalog(t, catalog, location.ID)
			if tt.want == nil {
				require.ErrorIs(t, err, cache.ErrNotFound)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, location, gotLocation)
			assert.Equal(t, tt.want, ingredients, "cached grades must be preserved without grading")
		})
	}
}

func TestCachedCatalogFailures(t *testing.T) {
	lat, lon := 44.98, -93.26
	location := &locations.Location{ID: "70100023", Lat: &lat, Lon: &lon}
	for _, tt := range []struct {
		name      string
		lookup    fakeLocationLookup
		cacheErr  error
		wantCalls int
	}{
		{"location", fakeLocationLookup{err: assert.AnError}, nil, 0},
		{"store date", fakeLocationLookup{location: &locations.Location{ID: "70100023"}}, nil, 0},
		{"corrupt cache", fakeLocationLookup{location: location}, assert.AnError, 1},
		{"canceled", fakeLocationLookup{location: location}, context.Canceled, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &failingIngredientCache{err: tt.cacheErr}
			catalog := NewStore(cache.NewInMemoryCache(), tt.lookup, c)
			_, _, err := loadTestCatalog(t, catalog, "70100023")
			require.Error(t, err)
			if tt.cacheErr != nil {
				require.ErrorIs(t, err, tt.cacheErr)
			}
			_, _, err = loadTestCatalog(t, catalog, "70100023")
			require.Error(t, err, "failed loads must be retried")
			assert.Equal(t, 2*tt.wantCalls, c.calls)
		})
	}
}

type countingIngredientCache struct {
	calls int
}

func (c *countingIngredientCache) IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error) {
	c.calls++
	return []ai.InputIngredient{{ProductID: "one", Grade: &ai.IngredientGrade{Score: c.calls, Reason: "Cached grade"}}}, nil
}

func newTestCachedCatalog() (*store, *countingIngredientCache) {
	lat, lon := 44.98, -93.26
	ingredients := &countingIngredientCache{}
	return NewStore(cache.NewInMemoryCache(), fakeLocationLookup{location: &locations.Location{ID: "70100023", Lat: &lat, Lon: &lon}}, ingredients), ingredients
}

func TestCatalogRetainedForThirtyMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		catalog, source := newTestCachedCatalog()
		_, first, err := loadTestCatalog(t, catalog, "70100023")
		require.NoError(t, err)
		time.Sleep(29 * time.Minute)
		_, cached, err := loadTestCatalog(t, catalog, "70100023")
		require.NoError(t, err)
		assert.Equal(t, first, cached)
		assert.Equal(t, 1, source.calls)
		time.Sleep(time.Minute)
		synctest.Wait()
		catalog.mu.Lock()
		retained := len(catalog.entries)
		catalog.mu.Unlock()
		assert.Zero(t, retained, "expired catalogs must be purged without another request")
		_, refreshed, err := loadTestCatalog(t, catalog, "70100023")
		require.NoError(t, err)
		assert.Equal(t, 2, source.calls)
		assert.Equal(t, 2, refreshed[0].Grade.Score)
	})
}

func TestCatalogLocationsExpireIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		catalog, source := newTestCachedCatalog()
		_, _, err := loadTestCatalog(t, catalog, "a")
		require.NoError(t, err)
		time.Sleep(15 * time.Minute)
		_, _, err = loadTestCatalog(t, catalog, "b")
		require.NoError(t, err)
		assert.Equal(t, 2, source.calls)
		time.Sleep(15 * time.Minute)
		synctest.Wait()
		catalog.mu.Lock()
		_, hasA := catalog.entries["a"]
		_, hasB := catalog.entries["b"]
		catalog.mu.Unlock()
		assert.False(t, hasA)
		assert.True(t, hasB)
	})
}

func TestConcurrentCatalogRequestsLoadOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		catalog, source := newTestCachedCatalog()
		var requests sync.WaitGroup
		for range 10 {
			requests.Go(func() {
				_, ingredients, err := loadTestCatalog(t, catalog, "70100023")
				assert.NoError(t, err)
				assert.Len(t, ingredients, 1)
			})
		}
		requests.Wait()
		assert.Equal(t, 1, source.calls)
	})
}

type fixedIngredientCache struct{ items []ai.InputIngredient }

func (c fixedIngredientCache) IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error) {
	return slices.Clone(c.items), nil
}

func TestRemoveReviewedReleasesItemsOnlyInActiveCatalog(t *testing.T) {
	shared := ai.InputIngredient{ProductID: "shared", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9, Reason: "Fresh"}, Embedding: ai.IngredientEmbedding{1, 2}}
	remaining := ai.InputIngredient{ProductID: "remaining", Grade: &ai.IngredientGrade{Score: 5, Reason: "Useful"}}
	lat, lon := 44.98, -93.26
	catalog := NewStore(cache.NewInMemoryCache(), fakeLocationLookup{location: &locations.Location{ID: "a", Lat: &lat, Lon: &lon}}, fixedIngredientCache{items: []ai.InputIngredient{shared, remaining}})
	_, _, err := loadTestCatalog(t, catalog, "a")
	require.NoError(t, err)
	_, _, err = loadTestCatalog(t, catalog, "b")
	require.NoError(t, err)
	require.NoError(t, catalog.save(t.Context(), "a", catalogGradeKey(shared), verdictCorrect, testReviewTime))
	_, items, err := loadTestCatalog(t, catalog, "a")
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{remaining}, items)
	// The unused backing-array slot must release all ingredient references.
	entry := catalog.entries["a"]
	backing := entry.ingredients[:cap(entry.ingredients)]
	assert.Equal(t, ai.InputIngredient{}, backing[1])
	_, otherStore, err := loadTestCatalog(t, catalog, "b")
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{shared, remaining}, otherStore)
	require.NoError(t, catalog.save(t.Context(), "a", catalogGradeKey(remaining), verdictCorrect, testReviewTime))
	assert.Nil(t, catalog.entries["a"].ingredients)
}

func TestNextRemovesReviewsFromOtherStores(t *testing.T) {
	first := ai.InputIngredient{ProductID: "first", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9}}
	second := ai.InputIngredient{ProductID: "second", Description: "Carrots", Grade: &ai.IngredientGrade{Score: 8}}
	next := ai.InputIngredient{ProductID: "next", Description: "Broccoli", Grade: &ai.IngredientGrade{Score: 7}}
	all := []ai.InputIngredient{first, second, next}
	lat, lon := 44.98, -93.26
	catalog := NewStore(cache.NewInMemoryCache(), fakeLocationLookup{location: &locations.Location{ID: "a", Lat: &lat, Lon: &lon}}, fixedIngredientCache{items: all})
	for _, id := range []string{"a", "b", "c"} {
		_, _, err := loadTestCatalog(t, catalog, id)
		require.NoError(t, err)
	}
	store := catalog
	for _, ingredient := range []ai.InputIngredient{first, second} {
		require.NoError(t, store.save(t.Context(), "a", catalogGradeKey(ingredient), verdictCorrect, testReviewTime))
	}
	response := httptest.NewRecorder()
	NewHandler(catalog).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=b", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Broccoli")
	assert.NotContains(t, response.Body.String(), "Asparagus")
	assert.NotContains(t, response.Body.String(), "Carrots")
	for _, id := range []string{"a", "b", "c"} {
		_, items, err := loadTestCatalog(t, catalog, id)
		require.NoError(t, err)
		if id == "a" || id == "b" {
			assert.Equal(t, []ai.InputIngredient{next}, items)
		} else {
			assert.Equal(t, all, items, "unvisited stores are not cleaned")
		}
	}
}

type reviewWriteCache struct {
	cache.Cache
	fail bool
}

func (c *reviewWriteCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if c.fail {
		return assert.AnError
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestReviewOnlyDropsItemAfterSave(t *testing.T) {
	ingredient := ai.InputIngredient{ProductID: "one", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: 9, Reason: "Fresh"}}
	lat, lon := 44.98, -93.26
	catalog := NewStore(cache.NewInMemoryCache(), fakeLocationLookup{location: &locations.Location{ID: "a", Lat: &lat, Lon: &lon}}, fixedIngredientCache{items: []ai.InputIngredient{ingredient}})
	c := &reviewWriteCache{Cache: cache.NewInMemoryCache(), fail: true}
	catalog.cache = c
	h := NewHandler(catalog)
	post := func() *httptest.ResponseRecorder {
		values := url.Values{"location": {"a"}, "grade_key": {catalogGradeKey(ingredient)}, "verdict": {"correct"}}
		r := httptest.NewRequest(http.MethodPost, "/grader/review", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		return response
	}
	require.Equal(t, http.StatusInternalServerError, post().Code)
	_, items, err := loadTestCatalog(t, catalog, "a")
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{ingredient}, items)
	c.fail = false
	require.Equal(t, http.StatusSeeOther, post().Code)
	_, items, err = loadTestCatalog(t, catalog, "a")
	require.NoError(t, err)
	assert.Empty(t, items)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/grader?location=a", nil))
	assert.Contains(t, response.Body.String(), "No grades left to review")
}

func TestCatalogReadConcurrentWithRemoval(t *testing.T) {
	catalog, _ := newTestCachedCatalog()
	_, items, err := loadTestCatalog(t, catalog, "a")
	require.NoError(t, err)
	key := catalogGradeKey(items[0])
	var requests sync.WaitGroup
	requests.Go(func() {
		for range 100 {
			_, _, err := catalog.Next(t.Context(), "a")
			assert.NoError(t, err)
		}
	})
	requests.Go(func() { assert.NoError(t, catalog.save(t.Context(), "a", key, verdictCorrect, testReviewTime)) })
	requests.Wait()
}
