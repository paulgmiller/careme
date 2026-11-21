package kroger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/providers/kroger/products"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wineTestProvider(t *testing.T, c cache.Cache, handler func(*http.Request) (*http.Response, error)) *staplesProvider {
	t.Helper()
	client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(handler)}))
	require.NoError(t, err)
	return newStaplesProviderWithCache(client, c)
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

type failingCatalogCache struct {
	cache.Cache
	readErr, writeErr error
}

func (c failingCatalogCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.Cache.Get(ctx, key)
}

func (c failingCatalogCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestWineCatalogRequiresReadableWritableCache(t *testing.T) {
	for _, stage := range []string{"read", "decode", "write"} {
		t.Run(stage, func(t *testing.T) {
			date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
			c := failingCatalogCache{Cache: cache.NewInMemoryCache()}
			switch stage {
			case "read":
				c.readErr = errors.New("read failed")
			case "write":
				c.writeErr = errors.New("write failed")
			case "decode":
				require.NoError(t, c.Put(t.Context(), wineCatalogCacheKey("70500874", date), "{", cache.Unconditional()))
			}
			provider := wineTestProvider(t, c, wineTestResponse)
			wines, err := provider.fetchWineCatalog(t.Context(), "70500874", date)
			require.ErrorContains(t, err, "Kroger wine catalog")
			assert.Nil(t, wines)
		})
	}
}
