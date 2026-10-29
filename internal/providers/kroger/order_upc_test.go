package kroger

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUPCResolutionConcurrentCachedAndOrdered(t *testing.T) {
	started := make(chan string, 10)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := make(map[string]int)
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		t.Error("unexpected cart write")
		return cartResponse(500, ""), nil
	})
	a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(r *http.Request) (*http.Response, error) {
		key := r.URL.Query().Get("filter.locationId") + r.URL.Path
		mu.Lock()
		calls[key]++
		mu.Unlock()
		started <- key
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		upc := "first"
		if r.URL.Path == "/v1/products/second" {
			upc = "second"
		}
		return cartResponse(200, `{"data":{"items":[{"itemId":"`+upc+`","inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}}]}}`), nil
	})}
	request := mustOrderRequest(t)
	request.Items = []ai.ShoppingItem{{ProductID: "first"}, {ProductID: "second"}, {ProductID: "first"}, {}}
	type outcome struct {
		items []CartItem
		gaps  []ai.ShoppingItem
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		items, gaps, err := a.resolveItems(context.Background(), request)
		done <- outcome{items, gaps, err}
	}()
	// Both requests must start before either is allowed to complete.
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("lookups did not overlap")
		}
	}
	close(release)
	first := <-done
	require.NoError(t, first.err)
	assert.Equal(t, []CartItem{{UPC: "first", Quantity: 1}, {UPC: "second", Quantity: 1}}, first.items)
	assert.Len(t, first.gaps, 1)
	items, _, err := a.resolveItems(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, first.items, items)
	assert.Empty(t, started, "cached mappings must not contact Kroger")
	request.Location.ID = "other-store"
	_, _, err = a.resolveItems(t.Context(), request)
	require.NoError(t, err)
	assert.Len(t, started, 2, "stores have separate mappings")
	mu.Lock()
	defer mu.Unlock()
	for _, count := range calls {
		assert.Equal(t, 1, count)
	}
}

func TestUPCCacheDoesNotStoreMissingProducts(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		t.Error("unexpected cart write")
		return cartResponse(500, ""), nil
	})
	calls := 0
	a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(404, `{}`), nil
	})}
	for range 2 {
		_, err := a.productUPC(t.Context(), "missing", "store")
		require.ErrorIs(t, err, ErrProductUnavailable)
	}
	assert.Equal(t, 2, calls)
}
