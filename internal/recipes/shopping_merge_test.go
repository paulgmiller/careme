package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingShoppingMerger struct{ calls int }

func (m *countingShoppingMerger) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
	m.calls++
	return (SimpleShoppingQuantityMerger{}).MergeShoppingQuantities(ctx, ingredients, catalog)
}

func TestMergedShoppingListCachedByFinalHash(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	ingredients := []ai.Ingredient{{ProductID: "garlic-id", Name: "Garlic", Quantity: "1 cloves"}, {ProductID: "garlic-id", Name: "Garlic", Quantity: "2 cloves"}}
	err := s.mergeAndCacheShoppingList(t.Context(), "final-hash", ingredients, []ai.InputIngredient{{ProductID: "garlic-id", Description: "Garlic", VariantID: "variant-42"}})
	require.NoError(t, err)
	first := cachedShoppingItems(t, s, "final-hash")
	purchases, err := s.cachedShoppingPurchases(t.Context(), "final-hash")
	require.NoError(t, err)
	assert.Equal(t, "variant-42", purchases[0].Items[0].VariantID)
	err = s.mergeAndCacheShoppingList(t.Context(), "final-hash", ingredients, []ai.InputIngredient{})
	require.NoError(t, err)
	second := cachedShoppingItems(t, s, "final-hash")
	assert.Equal(t, 1, merger.calls)
	assert.Equal(t, "1 cloves", first[0].Needed)
	assert.Len(t, first, 2)
	assert.Equal(t, first, second)
	reader, err := s.Cache.Get(t.Context(), shoppingQuantityCachePrefix+"final-hash")
	require.NoError(t, err, "store the merge directly under the finalized hash")
	require.NoError(t, reader.Close())

	updated := append(ingredients, ai.Ingredient{ProductID: "wine-id", Name: "Wine", Quantity: "1 bottle"})
	err = s.mergeAndCacheShoppingList(t.Context(), "final-hash", updated, []ai.InputIngredient{})
	require.NoError(t, err)
	cached := cachedShoppingItems(t, s, "final-hash")
	assert.Equal(t, first, cached, "the finalized list retains its first successful merge")
	assert.Equal(t, 1, merger.calls)

	err = s.mergeAndCacheShoppingList(t.Context(), "different-final-hash", updated, []ai.InputIngredient{})
	require.NoError(t, err)
	newList := cachedShoppingItems(t, s, "different-final-hash")
	assert.Equal(t, 2, merger.calls, "a different finalized hash gets its own merge")
	assert.NotEqual(t, first, newList)
}

type shoppingMergerFunc func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error)

func (f shoppingMergerFunc) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
	return f(ctx, ingredients, catalog)
}

func prepareShoppingFinalize(t *testing.T, s *server, p *generatorParams, recipes ...ai.Recipe) string {
	t.Helper()
	p.Location.Name = "Store"
	require.NoError(t, s.storage.Update(&utypes.User{ID: "mock-clerk-user-id", Email: []string{"chef@example.com"}, ShoppingDay: time.Saturday.String()}))
	hash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	saveRecipesForOrigin(t, s, hash, recipes...)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: recipes}, hash))
	require.NoError(t, s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", hash, recipeSelection{SavedHashes: []string{recipes[0].ComputeHash()}}))
	return hash
}

func finalizeShoppingRequest(s *server, hash string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+hash+"/finalize", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", hash)
	rr := httptest.NewRecorder()
	s.handleFinalize(rr, req)
	return rr
}

func TestFinalizeRetriesMergeFailure(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "1 clove"}}}
	hash := prepareShoppingFinalize(t, s, p, recipe)
	p.Saved = []ai.Recipe{recipe}
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		return nil, errors.New("AI unavailable")
	})
	rr := finalizeShoppingRequest(s, hash)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "Try again, chef")
	assert.Empty(t, rr.Header().Get("HX-Redirect"))
	_, err := s.FromCache(t.Context(), p.Hash())
	require.ErrorIs(t, err, cache.ErrNotFound, "do not finalize a list without its merge")
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	rr = finalizeShoppingRequest(s, hash)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("HX-Redirect"))
	assert.Equal(t, 1, merger.calls)
	_, err = s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
}

func TestShoppingPurchaseCacheRetainsFinalizedSnapshot(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	needs := []ai.Ingredient{{ProductID: "pork", Name: "Pork", Quantity: "24 oz"}}
	price := float32(6)
	catalog := []ai.InputIngredient{{ProductID: "pork", Description: "Pork", Size: "32 oz", PriceRegular: &price}}
	err := s.mergeAndCacheShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	first := cachedShoppingItems(t, s, "plan")
	err = s.mergeAndCacheShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	second := cachedShoppingItems(t, s, "plan")
	assert.Equal(t, first, second)
	assert.Equal(t, 1, merger.calls)
	catalog[0].Size = "16 oz"
	err = s.mergeAndCacheShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 1, merger.calls)
	sale := float32(3)
	catalog[0].PriceSale = &sale
	err = s.mergeAndCacheShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 1, merger.calls)
	catalog[0].Grade = &ai.IngredientGrade{Score: 10}
	catalog[0].Embedding = ai.IngredientEmbedding{1, 2}
	err = s.mergeAndCacheShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 1, merger.calls, "catalog changes should not regenerate the finalized snapshot")
}

func TestFinalizedShoppingInputsIgnorePantryAndUseSavedStoreAndDate(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	p.Saved = []ai.Recipe{{Title: "Dinner", WineStyles: []string{"red"}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	staples := []ai.InputIngredient{{ProductID: "pork", Description: "Pork", Size: "16 oz"}}
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), staples))
	// Wine recommendations and unused product catalogs must not affect merging.
	require.NoError(t, s.Cache.Put(t.Context(), recipeWineCacheKey(p.Saved[0].ComputeHash()), "invalid JSON", cache.Unconditional()))
	require.NoError(t, s.Cache.Put(t.Context(), ingredientsCachePrefix+"pantry/query-categories-v1/"+p.LocationHash(), "invalid JSON", cache.Unconditional()))
	require.NoError(t, s.Cache.Put(t.Context(), ingredientsCachePrefix+wineStylesCacheKey([]string{"red"}, p.Location.ID, p.Date), "invalid JSON", cache.Unconditional()))
	_, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, staples, catalog)
	otherDate := *p
	otherDate.Date = p.Date.AddDate(0, 0, 1)
	otherStore := *p
	otherStore.Location = &locations.Location{ID: "walmart_999"}
	for _, other := range []generatorParams{otherDate, otherStore} {
		require.NoError(t, s.SaveParams(t.Context(), &other))
		_, catalog, err = s.finalizedShoppingInputs(t.Context(), other.Hash())
		require.NoError(t, err)
		assert.Empty(t, catalog)
	}
}

func TestShoppingCatalogCorruptionStopsMerge(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Pork", Quantity: "24 oz"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.Cache.Put(t.Context(), ingredientsCachePrefix+p.LocationHash(), "invalid JSON", cache.Unconditional()))
	_, _, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.ErrorContains(t, err, "load shopping catalog")
}

func TestShoppingPurchaseSectionShowsRecommendationsAndUnresolvedNeeds(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "pork", Name: "Pork", Quantity: "24 oz"}, {Name: "Saffron", Quantity: "1 pinch"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), []ai.InputIngredient{{ProductID: "pork", Description: "Pork & herbs", Size: "32 oz", AisleNumber: "Meat"}}))
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		require.Len(t, catalog, 1)
		return []ai.ShoppingItem{{ProductID: needs[0].ProductID, Name: needs[0].Name, Needed: needs[0].Quantity, Buy: new(float64(1))}, {Name: needs[1].Name, Needed: needs[1].Quantity}}, nil
	})
	originParams := DefaultParams(p.Location, p.Date)
	hash := prepareShoppingFinalize(t, s, originParams, p.Saved...)
	rr := finalizeShoppingRequest(s, hash)
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("HX-Redirect"), p.Hash())

	// The finalized list must remain usable without any catalog snapshots.
	require.NoError(t, s.Cache.Put(t.Context(), ingredientsCachePrefix+p.LocationHash(), "invalid JSON", cache.Unconditional()))
	// A page load must use the snapshot without another model call.
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		t.Fatal("page load must not invoke the merger")
		return nil, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil)
	rr = httptest.NewRecorder()
	s.renderShoppingList(rr, req, p, &ai.ShoppingList{Recipes: p.Saved}, nil, shoppingProgress{})
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Pork &amp; herbs")
	assert.Contains(t, body, "Buy 1 × 32 oz")
	assert.NotContains(t, body, "Needed for recipes")
	assert.Contains(t, body, "Needed: 24 oz")
	assert.Contains(t, body, "Saffron")
	assert.Contains(t, body, "1 pinch")
	assert.NotContains(t, body, "/shopping-quantities")
	isValidHTML(t, body)
}

func TestShoppingPurchaseSectionPreservesCatalogSizes(t *testing.T) {
	for _, size := range []string{"avg 1.03lb", "1 lb (sold by weight)", "24 ct", "12 eggs", "1 basket"} {
		t.Run(size, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "product", Name: "Ingredient", Quantity: "1 serving"}}}}
			require.NoError(t, s.SaveParams(t.Context(), p))
			catalog := []ai.InputIngredient{{ProductID: "product", Description: "Ingredient", Size: size, AisleNumber: "Produce"}}
			s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
				return []ai.ShoppingItem{{ProductID: "product", Name: "Ingredient", Needed: "1 serving", Buy: new(float64(1))}}, nil
			})
			require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), p.Saved[0].Ingredients, catalog))
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil)
			s.renderShoppingList(rr, req, p, &ai.ShoppingList{Recipes: p.Saved}, nil, shoppingProgress{})
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Contains(t, rr.Body.String(), "Buy 1 × "+size)
		})
	}
}

func TestShoppingPageAppendsLateWinePicksWithoutMerging(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{
		{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Pork", Quantity: "24 oz"}}},
		{Title: "Second dinner", Ingredients: []ai.Ingredient{{Name: "Pork", Quantity: "16 oz"}}},
	}
	require.NoError(t, s.SaveParams(t.Context(), p))
	needs, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, []ai.Ingredient{p.Saved[0].Ingredients[0], p.Saved[1].Ingredients[0]}, needs)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), needs, catalog))
	before, err := s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	require.Len(t, before, 1)

	render := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil)
		rr := httptest.NewRecorder()
		s.renderShoppingList(rr, req, p, &ai.ShoppingList{Recipes: p.Saved}, nil, shoppingProgress{})
		require.Equal(t, http.StatusOK, rr.Code)
		isValidHTML(t, rr.Body.String())
		return rr.Body.String()
	}
	assert.NotContains(t, render(), "Red wine")
	wine := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	for _, recipe := range p.Saved {
		require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{wine, {Name: "Other wine"}}}))
	}
	body := render()
	require.Contains(t, body, `id="shopping-list-section"`)
	purchaseSection := body[strings.Index(body, `id="shopping-list-section"`):]
	assert.Equal(t, 2, strings.Count(purchaseSection, "Red wine"))
	assert.Equal(t, 2, strings.Count(purchaseSection, "1 bottle"))
	assert.NotContains(t, purchaseSection, "Other wine")
	assert.NotContains(t, purchaseSection, "Buy ")
	after, err := s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, 1, merger.calls, "page loads must not invoke the merger")
}

func TestShoppingMergeFiltersCatalogBeforeCallAndCache(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		calls++
		require.Equal(t, []ai.InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}}, catalog)
		require.Len(t, needs, 3, "send raw needs without deduplication")
		return []ai.ShoppingItem{{ProductID: "onion", Name: "Onion", Needed: "8 oz", Buy: new(float64(1))}, {Name: "Olive oil", Needed: "4½ tbsp"}}, nil
	})
	needs := []ai.Ingredient{{ProductID: "onion", Name: "Onion", Quantity: "4 oz"}, {ProductID: "onion", Name: "Onions", Quantity: "4 oz"}, {Name: "Olive oil", Quantity: "4½ tbsp"}}
	catalog := []ai.InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}, {ProductID: "other-onion", Description: "Onion", Size: "2 lb"}, {ProductID: "oil", Description: "Olive oil", Size: "16 fl oz"}}
	err := s.mergeAndCacheShoppingList(t.Context(), "filtered", needs, catalog)
	require.NoError(t, err)
	first := cachedShoppingItems(t, s, "filtered")
	catalog[1].Size = "3 lb"
	err = s.mergeAndCacheShoppingList(t.Context(), "filtered", needs, catalog)
	require.NoError(t, err)
	second := cachedShoppingItems(t, s, "filtered")
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls, "unmatched catalog metadata does not invalidate the merge")
}

func cachedShoppingItems(t *testing.T, s *server, hash string) []ai.ShoppingItem {
	t.Helper()
	groups, err := s.cachedShoppingPurchases(t.Context(), hash)
	require.NoError(t, err)
	var items []ai.ShoppingItem
	for _, group := range groups {
		for _, item := range group.Items {
			items = append(items, item.ShoppingItem)
		}
	}
	return items
}
