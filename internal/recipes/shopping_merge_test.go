package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingShoppingMerger struct{ calls int }

func (m *countingShoppingMerger) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.Ingredient, error) {
	m.calls++
	return (SimpleShoppingQuantityMerger{}).MergeShoppingQuantities(ctx, ingredients, catalog)
}

func TestMergedShoppingListCachedByFinalHash(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	ingredients := []ai.Ingredient{{ProductID: "garlic-id", Name: "Garlic", Quantity: "1 cloves"}, {ProductID: "garlic-id", Name: "Garlic", Quantity: "2 cloves"}}
	first, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients, []ai.InputIngredient{})
	require.NoError(t, err)
	second, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients, []ai.InputIngredient{})
	require.NoError(t, err)
	assert.Equal(t, 1, merger.calls)
	assert.Equal(t, "3 cloves", first[0].Items[0].Quantity)
	assert.Equal(t, first, second)
	_, err = s.mergedShoppingList(t.Context(), "final-hash", append(ingredients, ai.Ingredient{ProductID: "wine-id", Name: "Wine", Quantity: "1 bottle"}), []ai.InputIngredient{})
	require.NoError(t, err)
	assert.Equal(t, 2, merger.calls)
	ingredients[0].Price = "$3"
	updated, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients, []ai.InputIngredient{})
	require.NoError(t, err)
	assert.Equal(t, 3, merger.calls)
	assert.Equal(t, "$3", updated[0].Items[0].Price)
}

func TestFinalizedShoppingSection(t *testing.T) {
	for _, id := range []string{"01400943", "walmart_123", "mnfoodclub_delivery"} {
		t.Run(id, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: id}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}, {ProductID: "garlic", Name: "Garlic", Quantity: "1 cloves"}}}}
			require.NoError(t, s.SaveParams(t.Context(), p))
			req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
			req.SetPathValue("hash", p.Hash())
			rr := httptest.NewRecorder()
			s.handleShoppingQuantities(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Contains(t, rr.Body.String(), "3 cloves")
			assert.NotContains(t, rr.Body.String(), "cart")
			isValidHTML(t, rr.Body.String())
		})
	}
}

type shoppingMergerFunc func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.Ingredient, error)

func (f shoppingMergerFunc) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.Ingredient, error) {
	return f(ctx, ingredients, catalog)
}

func TestShoppingQuantitiesRetriesMergeFailure(t *testing.T) {
	s := newTestServer(t)
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.Ingredient, error) {
		return nil, errors.New("AI unavailable")
	})
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "1 clove"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Try again, chef")
	assert.Contains(t, rr.Body.String(), `/recipes/`+p.Hash()+`/shopping-quantities`)
	assert.NotContains(t, rr.Body.String(), "1 clove")
}

func TestShoppingQuantitiesRejectsUnfinalizedList(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestShoppingPurchaseCacheTracksCatalogMetadata(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	needs := []ai.Ingredient{{ProductID: "pork", Name: "Pork", Quantity: "24 oz"}}
	price := float32(6)
	catalog := []ai.InputIngredient{{ProductID: "pork", Description: "Pork", Size: "32 oz", PriceRegular: &price}}
	first, err := s.mergedShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	second, err := s.mergedShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, merger.calls)
	catalog[0].Size = "16 oz"
	_, err = s.mergedShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 2, merger.calls)
	sale := float32(3)
	catalog[0].PriceSale = &sale
	_, err = s.mergedShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 3, merger.calls)
	catalog[0].Grade = &ai.IngredientGrade{Score: 10}
	catalog[0].Embedding = ai.IngredientEmbedding{1, 2}
	_, err = s.mergedShoppingList(t.Context(), "plan", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, 3, merger.calls, "non-purchasing metadata should not trigger a merge")
}

func TestCachedShoppingCatalogUsesSavedStoreAndDate(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	p.Saved = []ai.Recipe{{Title: "Dinner", WineStyles: []string{"red"}}, {Title: "Second dinner", WineStyles: []string{"red"}}}
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), []ai.InputIngredient{{ProductID: "small", Description: "Pork", Size: "16 oz"}, {ProductID: "large", Description: "Pork", Size: "32 oz", Grade: &ai.IngredientGrade{Score: 10}, Embedding: ai.IngredientEmbedding{1}}}))
	require.NoError(t, s.SaveIngredients(t.Context(), "pantry/query-categories-v1/"+p.LocationHash(), []ai.InputIngredient{{ProductID: "salt", Description: "Salt"}, {ProductID: "small", Description: "Pork", Size: "16 oz"}}))
	require.NoError(t, s.SaveIngredients(t.Context(), wineStylesCacheKey([]string{"red"}, p.Location.ID, p.Date), []ai.InputIngredient{{ProductID: "wine", Description: "Red wine", Size: "750 ml"}}))
	got, err := s.cachedShoppingCatalog(t.Context(), p)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []string{"small", "large", "salt", "wine"}, []string{got[0].ProductID, got[1].ProductID, got[2].ProductID, got[3].ProductID})
	assert.Nil(t, got[1].Grade)
	assert.Nil(t, got[1].Embedding)
	other := *p
	other.Date = p.Date.AddDate(0, 0, 1)
	got, err = s.cachedShoppingCatalog(t.Context(), &other)
	require.NoError(t, err)
	assert.Empty(t, got)
	other = *p
	other.Location = &locations.Location{ID: "walmart_999"}
	got, err = s.cachedShoppingCatalog(t.Context(), &other)
	require.NoError(t, err)
	assert.Empty(t, got)
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
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.Ingredient, error) {
		require.Len(t, catalog, 1)
		return []ai.Ingredient{{ProductID: needs[0].ProductID, Name: needs[0].Name, Quantity: "32 oz"}, needs[1]}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Pork &amp; herbs")
	assert.Contains(t, body, "32 oz")
	assert.Contains(t, body, "32 oz")
	assert.NotContains(t, body, "Needed for recipes")
	assert.NotContains(t, body, "24 oz")
	assert.Contains(t, body, "Saffron")
	assert.Contains(t, body, "1 pinch")
	isValidHTML(t, body)
}

func TestFinalizedShoppingInputsIncludeWine(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", WineStyles: []string{"red"}, Ingredients: []ai.Ingredient{{Name: "Pork", Quantity: "24 oz"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	wine := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	require.NoError(t, s.SaveWine(t.Context(), p.Saved[0].ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{wine}}))
	require.NoError(t, s.SaveIngredients(t.Context(), wineStylesCacheKey([]string{"red"}, p.Location.ID, p.Date), []ai.InputIngredient{{ProductID: "wine", Description: "Red wine", Size: "750 ml"}}))
	needs, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Contains(t, needs, wine)
	require.Len(t, catalog, 1)
	assert.Equal(t, "wine", catalog[0].ProductID)
}

func TestShoppingMergeFiltersCatalogBeforeCallAndCache(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.Ingredient, error) {
		calls++
		require.Equal(t, []ai.InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}}, catalog)
		require.Len(t, needs, 3, "send raw needs without deduplication")
		return []ai.Ingredient{{ProductID: "onion", Name: "Onion", Quantity: "1 lb"}, {Name: "Olive oil", Quantity: "4½ tbsp"}}, nil
	})
	needs := []ai.Ingredient{{ProductID: "onion", Name: "Onion", Quantity: "4 oz"}, {ProductID: "onion", Name: "Onions", Quantity: "4 oz"}, {Name: "Olive oil", Quantity: "4½ tbsp"}}
	catalog := []ai.InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}, {ProductID: "other-onion", Description: "Onion", Size: "2 lb"}, {ProductID: "oil", Description: "Olive oil", Size: "16 fl oz"}}
	first, err := s.mergedShoppingList(t.Context(), "filtered", needs, catalog)
	require.NoError(t, err)
	catalog[1].Size = "3 lb"
	second, err := s.mergedShoppingList(t.Context(), "filtered", needs, catalog)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, calls, "unmatched catalog metadata does not invalidate the merge")
}
