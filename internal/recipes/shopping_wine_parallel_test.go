package recipes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockedWineReadCache struct {
	cache.ListCache
	started chan string
	release chan struct{}
}

func (c *blockedWineReadCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.HasPrefix(key, wineRecommendationsCachePrefix) || strings.HasPrefix(key, selectedWineIngredientsPrefix) {
		c.started <- key
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.ListCache.Get(ctx, key)
}

func TestShoppingWineAndCatalogReadsOverlapAcrossRecipes(t *testing.T) {
	store := &blockedWineReadCache{ListCache: cache.NewInMemoryCache(), started: make(chan string, 4), release: make(chan struct{})}
	s := newTestServer(t, withTestCache(store))
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "First dinner"}, {Title: "Second dinner"}}
	require.NoError(t, store.Put(t.Context(), shoppingQuantityCachePrefix+p.Hash(), `[]`, cache.Unconditional()))
	for _, recipe := range p.Saved {
		require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}}}))
		require.NoError(t, s.saveWineIngredients(t.Context(), recipe.ComputeHash(), []ai.InputIngredient{{ProductID: "wine", VariantID: "upc", Description: "Red wine", Size: "750mL"}}))
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.renderShoppingList(rr, req, p, &ai.ShoppingList{Recipes: p.Saved}, nil, shoppingProgress{})
	}()
	// Both recommendations and both catalogs must start before any can finish.
	keys := make(map[string]bool)
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for range 4 {
		select {
		case key := <-store.started:
			keys[key] = true
		case <-timeout.C:
			close(store.release)
			<-done
			t.Fatal("wine and catalog reads did not overlap")
		}
	}
	close(store.release)
	<-done
	assert.Len(t, keys, 4)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "Red wine")
}

func TestShoppingWineCatalogReadFailure(t *testing.T) {
	for _, finalized := range []bool{true, false} {
		name := "unfinalized"
		if finalized {
			name = "finalized"
		}
		t.Run(name, func(t *testing.T) {
			store := cache.NewInMemoryCache()
			s := newTestServer(t, withTestCache(store))
			p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner"}}
			if finalized {
				require.NoError(t, store.Put(t.Context(), shoppingQuantityCachePrefix+p.Hash(), `[]`, cache.Unconditional()))
			}
			require.NoError(t, store.Put(t.Context(), selectedWineIngredientsPrefix+p.Saved[0].ComputeHash(), `{`, cache.Unconditional()))
			rr := httptest.NewRecorder()
			s.renderShoppingList(rr, httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil), p, &ai.ShoppingList{Recipes: p.Saved}, nil, shoppingProgress{})
			if finalized {
				assert.Equal(t, http.StatusInternalServerError, rr.Code)
			} else {
				assert.Equal(t, http.StatusOK, rr.Code)
			}
		})
	}
}
