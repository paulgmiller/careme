package kroger

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/providers/kroger/products"
	"careme/internal/recipes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wineTestProvider(t *testing.T, c cache.Cache, handler func(*http.Request) (*http.Response, error)) *wineStaplesProvider {
	t.Helper()
	client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}))
	require.NoError(t, err)
	return cachedWineTestProvider(client, c)
}

func cachedWineTestProvider(client *products.ClientWithResponses, c cache.Cache) *wineStaplesProvider {
	raw := &staplesProvider{client: client}
	return WithWineCatalog(raw, recipes.NewCachedWineProvider([]recipes.StaplesBackend{raw}, c))
}

func wineTestResponse(req *http.Request) (*http.Response, error) {
	term := req.URL.Query().Get("filter.term")
	return jsonResponse(req, http.StatusOK, fmt.Sprintf(`{"data":[{"productId":%q,"description":%q,"categories":["Adult Beverage"],"items":[{"itemId":%q,"size":"750mL","price":{"regular":12.99}}]}]}`, term, term, term+"-upc")), nil
}

func TestWineCatalogCachedAcrossStylesAndProviderInstances(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	handler := func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Query().Get("filter.term")]++
		mu.Unlock()
		return wineTestResponse(req)
	}
	store := cache.NewInMemoryCache()
	for _, style := range []string{"Pinot Noir", "Albariño"} {
		provider := wineTestProvider(t, store, handler)
		wines, err := provider.FetchWines(t.Context(), "70500874", []string{style})
		require.NoError(t, err)
		require.Len(t, wines, 3)
		assert.Equal(t, style, wines[0].ProductID, "targeted matches stay first")
		assert.Equal(t, style+"-upc", wines[0].VariantID)
	}
	assert.Equal(t, map[string]int{"Pinot Noir": 1, "Albariño": 1, "red wine": 1, "white wine": 1}, calls)
}

func TestWineCatalogCacheSeparatesStoreAndLookupDay(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	provider := wineTestProvider(t, cache.NewInMemoryCache(), func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return wineTestResponse(req)
	})
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		location string
		date     time.Time
	}{
		{"70500874", date}, {"70500874", date}, {"70100023", date}, {"70500874", date.AddDate(0, 0, 1)},
	} {
		wines, err := provider.fetchWineCatalog(t.Context(), tc.location, tc.date)
		require.NoError(t, err)
		require.Len(t, wines, 2)
	}
	assert.Equal(t, 6, calls)
}

func TestWineCatalogFetchRequiresBothSources(t *testing.T) {
	for _, failedTerm := range []string{"Pinot Noir", "red wine", "white wine"} {
		t.Run(failedTerm, func(t *testing.T) {
			provider := wineTestProvider(t, cache.NewInMemoryCache(), func(req *http.Request) (*http.Response, error) {
				if req.URL.Query().Get("filter.term") == failedTerm {
					return jsonResponse(req, http.StatusBadRequest, `{"errors":{"reason":"search failed"}}`), nil
				}
				return wineTestResponse(req)
			})
			wines, err := provider.FetchWines(t.Context(), "70500874", []string{"Pinot Noir"})
			require.ErrorContains(t, err, "search failed")
			assert.Nil(t, wines, "do not return a partial candidate catalog")
		})
	}
}

type failingCatalogCache struct{ cache.Cache }

func (c failingCatalogCache) Put(context.Context, string, string, cache.PutOptions) error {
	return errors.New("catalog write failed")
}

func TestWineCatalogRequiresSharedCacheWrite(t *testing.T) {
	provider := wineTestProvider(t, failingCatalogCache{cache.NewInMemoryCache()}, wineTestResponse)
	wines, err := provider.FetchWines(t.Context(), "70500874", []string{"Pinot Noir"})
	require.ErrorContains(t, err, "catalog write failed")
	assert.Nil(t, wines)
}

func TestSharedCacheWrapsRawAndCombinedWineQueries(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	store := cache.NewInMemoryCache()
	combined := wineTestProvider(t, store, func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Query().Get("filter.term")]++
		mu.Unlock()
		return wineTestResponse(req)
	})
	outer := recipes.NewCachedWineProvider([]recipes.StaplesBackend{combined}, store)
	date := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	for _, style := range []string{"Pinot Noir", "Albariño", "Pinot Noir"} {
		wines, err := outer.FetchWines(t.Context(), "70500874", []string{style}, date)
		require.NoError(t, err)
		require.Len(t, wines, 3)
		assert.Equal(t, style, wines[0].ProductID)
		assert.Equal(t, style+"-upc", wines[0].VariantID)
	}
	assert.Equal(t, map[string]int{"Pinot Noir": 1, "Albariño": 1, "red wine": 1, "white wine": 1}, calls)
}

func TestSharedBroadCatalogCoalescesConcurrentRecipeStyles(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	store := cache.NewInMemoryCache()
	combined := wineTestProvider(t, store, func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[req.URL.Query().Get("filter.term")]++
		mu.Unlock()
		return wineTestResponse(req)
	})
	outer := recipes.NewCachedWineProvider([]recipes.StaplesBackend{combined}, store)
	date := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	errs := make(chan error, 12)
	for i := range 12 {
		go func() {
			<-start
			_, err := outer.FetchWines(t.Context(), "70500874", []string{fmt.Sprintf("style-%d", i)}, date)
			errs <- err
		}()
	}
	close(start)
	for range 12 {
		require.NoError(t, <-errs)
	}
	assert.Equal(t, 1, calls["red wine"])
	assert.Equal(t, 1, calls["white wine"])
	assert.Len(t, calls, 14)
}
