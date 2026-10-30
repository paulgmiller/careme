package recipes

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type wineTestGenerator struct {
	generator
	pick func(context.Context, string, ai.Recipe, time.Time) (*ai.WineSelection, error)
}

func (g wineTestGenerator) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	return g.pick(ctx, location, recipe, date)
}

func finalizedWineTestParams(t *testing.T, s *server) *generatorParams {
	t.Helper()
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", WineStyles: []string{"Red"}, Ingredients: []ai.Ingredient{{ProductID: "food", Name: "Food", Quantity: "1 lb"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), []ai.InputIngredient{{ProductID: "food", Description: "Food", VariantID: "food-upc", Size: "1 lb"}}))
	return p
}

func TestFinalizePollsWineProducedByAnotherServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := cache.NewInMemoryCache()
		s := newTestServer(t, withTestCache(store))
		producer := newTestServer(t, withTestCache(store))
		p := finalizedWineTestParams(t, s)
		recipe := p.Saved[0]
		started, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		producer.generator = wineTestGenerator{generator: producer.generator, pick: func(ctx context.Context, _ string, _ ai.Recipe, _ time.Time) (*ai.WineSelection, error) {
			calls.Add(1)
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if err := producer.saveWineIngredients(ctx, recipe.ComputeHash(), []ai.InputIngredient{{ProductID: "wine", VariantID: "wine-upc", Description: "Selected red", Size: "750mL"}}); err != nil {
				return nil, err
			}
			return &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Selected red", Quantity: "750mL"}, {ProductID: "backup", Name: "Backup"}}}, nil
		}}
		backgroundDone := make(chan struct{})
		go func() {
			defer close(backgroundDone)
			producer.ensureSavedRecipeWine(t.Context(), recipe.ComputeHash(), p.Location.ID, recipe, p.Date)
		}()
		<-started
		finalized := make(chan error, 1)
		go func() { finalized <- s.finalizeShoppingPurchases(t.Context(), p.Hash()) }()
		synctest.Wait()
		select {
		case err := <-finalized:
			t.Fatalf("finalization returned before the wine pick completed: %v", err)
		default:
		}
		assert.Equal(t, int32(1), calls.Load())
		close(release)
		require.NoError(t, <-finalized)
		<-backgroundDone
		assert.Equal(t, int32(1), calls.Load())
		items := cachedShoppingItems(t, s, p.Hash())
		require.Len(t, items, 2)
		assert.Equal(t, "wine-upc", items[1].VariantID)
		assert.Equal(t, "1 bottle", items[1].Needed)
		// Even if wine recommendations later change, page preparation never alters this snapshot.
		require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "other", Name: "Other wine"}}}))
		require.NoError(t, s.finalizeShoppingPurchases(t.Context(), p.Hash()))
		assert.Equal(t, items, cachedShoppingItems(t, s, p.Hash()))
	})
}

func TestFinalizeWineWaitTimesOutAfterThirtySeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
		p := finalizedWineTestParams(t, s)
		s.generator = wineTestGenerator{generator: s.generator, pick: func(ctx context.Context, _ string, _ ai.Recipe, _ time.Time) (*ai.WineSelection, error) {
			t.Error("finalization must not generate wine")
			return nil, errors.New("unexpected wine generation")
		}}
		started := time.Now()
		err := s.finalizeShoppingPurchases(t.Context(), p.Hash())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 30*time.Second, time.Since(started))
		_, err = s.cachedShoppingPurchases(t.Context(), p.Hash())
		require.ErrorIs(t, err, cache.ErrNotFound)
	})
}

type failedWineSaveCache struct{ cache.ListCache }

func (c failedWineSaveCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if len(key) >= len(wineRecommendationsCachePrefix) && key[:len(wineRecommendationsCachePrefix)] == wineRecommendationsCachePrefix {
		return errors.New("wine write failed")
	}
	return c.ListCache.Put(ctx, key, value, opts)
}

func TestFinalizeEmptyWineIsCompleteAndFailuresAreRetryable(t *testing.T) {
	for _, scenario := range []string{"empty cached result", "generation failure", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := cache.NewInMemoryCache()
				s := newTestServer(t, withTestCache(store))
				var producerStore cache.ListCache = store
				if scenario == "save failure" {
					producerStore = failedWineSaveCache{store}
				}
				producer := newTestServer(t, withTestCache(producerStore))
				p := finalizedWineTestParams(t, s)
				recipe := p.Saved[0]
				calls := 0
				producer.generator = wineTestGenerator{generator: producer.generator, pick: func(context.Context, string, ai.Recipe, time.Time) (*ai.WineSelection, error) {
					calls++
					if scenario == "generation failure" && calls == 1 {
						return nil, errors.New("wine generation failed")
					}
					return &ai.WineSelection{Wines: []ai.Ingredient{}}, nil
				}}
				if scenario == "empty cached result" {
					require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{}, Commentary: "No matches."}))
				} else {
					producer.ensureSavedRecipeWine(t.Context(), recipe.ComputeHash(), p.Location.ID, recipe, p.Date)
				}
				err := s.finalizeShoppingPurchases(t.Context(), p.Hash())
				if scenario == "empty cached result" {
					require.NoError(t, err)
					assert.Zero(t, calls)
					return
				}
				require.ErrorIs(t, err, context.DeadlineExceeded)
				_, loadErr := s.cachedShoppingPurchases(t.Context(), p.Hash())
				require.ErrorIs(t, loadErr, cache.ErrNotFound)
				// Once a producer can save the result, retrying finalization succeeds.
				producer.Cache = store
				producer.ensureSavedRecipeWine(t.Context(), recipe.ComputeHash(), p.Location.ID, recipe, p.Date)
				require.NoError(t, s.finalizeShoppingPurchases(t.Context(), p.Hash()))
				assert.Equal(t, 2, calls)
			})
		})
	}
}

func TestFinalizeMergesFoodAndRepeatedWineInOneCall(t *testing.T) {
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
	p := finalizedWineTestParams(t, s)
	p.Saved = append(p.Saved, ai.Recipe{Title: "Second dinner", Ingredients: []ai.Ingredient{{ProductID: "food", Name: "Food", Quantity: "1 lb"}}})
	require.NoError(t, s.SaveParams(t.Context(), p))
	// The finalized hash changed with the saved recipes; retain its food catalog.
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), []ai.InputIngredient{{ProductID: "food", VariantID: "food-upc", Description: "Food", Size: "1 lb"}}))
	for _, recipe := range p.Saved {
		require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Red"}, {ProductID: "backup", Name: "Backup"}}}))
		require.NoError(t, s.saveWineIngredients(t.Context(), recipe.ComputeHash(), []ai.InputIngredient{{ProductID: "wine", VariantID: "wine-upc", Description: "Red", Size: "750mL"}}))
	}
	calls := 0
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		calls++
		require.Len(t, needs, 4)
		assert.Equal(t, "food", needs[0].ProductID)
		assert.Equal(t, "food", needs[1].ProductID)
		assert.Equal(t, "1 bottle", needs[2].Quantity)
		assert.Equal(t, "1 bottle", needs[3].Quantity)
		return []ai.ShoppingItem{{ProductID: "food", Name: "Food", Needed: "2 lb", Buy: new(float64(2))}, {ProductID: "wine", Name: "Red", Needed: "2 bottles", Buy: new(float64(2))}}, nil
	})
	require.NoError(t, s.finalizeShoppingPurchases(t.Context(), p.Hash()))
	require.NoError(t, s.finalizeShoppingPurchases(t.Context(), p.Hash()))
	assert.Equal(t, 1, calls)
	items := cachedShoppingItems(t, s, p.Hash())
	require.Len(t, items, 2)
	assert.Equal(t, "wine-upc", items[1].VariantID)
	assert.Equal(t, 2, items[1].PackageQuantity())
}
