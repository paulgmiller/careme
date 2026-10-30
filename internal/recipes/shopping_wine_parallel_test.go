package recipes

import (
	"context"
	"io"
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
	foodKey         string
	started         chan string
	release         chan struct{}
	catalogs        chan string
	releaseCatalogs chan struct{}
}

func (c *blockedWineReadCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	var release chan struct{}
	if key == c.foodKey || strings.HasPrefix(key, wineRecommendationsCachePrefix) {
		c.started <- key
		release = c.release
	}
	if strings.HasPrefix(key, ingredientsCachePrefix+"wines/") {
		c.catalogs <- key
		release = c.releaseCatalogs
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.ListCache.Get(ctx, key)
}

func TestFinalizeFoodAndWineReadsOverlapAcrossRecipes(t *testing.T) {
	store := &blockedWineReadCache{ListCache: cache.NewInMemoryCache(), started: make(chan string, 3), release: make(chan struct{}), catalogs: make(chan string, 2), releaseCatalogs: make(chan struct{})}
	s := newTestServer(t, withTestCache(store))
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "First dinner"}, {Title: "Second dinner"}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	store.foodKey = ingredientsCachePrefix + p.LocationHash()
	for _, recipe := range p.Saved {
		require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Red wine"}}}))
		require.NoError(t, s.SaveIngredients(t.Context(), wineStylesCacheKey(recipe.WineStyles, p.Location.ID, p.Date), []ai.InputIngredient{{ProductID: "wine", VariantID: "upc", Description: "Red wine", Size: "750mL"}}))
	}
	type result struct {
		ingredients []ai.Ingredient
		err         error
	}
	done := make(chan result, 1)
	go func() {
		ingredients, _, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
		done <- result{ingredients, err}
	}()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for range 3 {
		select {
		case <-store.started:
		case <-timeout.C:
			close(store.release)
			close(store.releaseCatalogs)
			<-done
			t.Fatal("food and wine reads did not overlap")
		}
	}
	close(store.release)
	for range 2 {
		select {
		case <-store.catalogs:
		case <-timeout.C:
			close(store.releaseCatalogs)
			<-done
			t.Fatal("wine catalog reads did not overlap")
		}
	}
	close(store.releaseCatalogs)
	got := <-done
	require.NoError(t, got.err)
	require.Len(t, got.ingredients, 2)
	assert.Equal(t, "1 bottle", got.ingredients[0].Quantity)
	assert.Equal(t, "1 bottle", got.ingredients[1].Quantity)
}
