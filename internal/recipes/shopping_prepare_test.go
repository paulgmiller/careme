package recipes

import (
	"context"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareShoppingItemsUsesFoodAndWineCatalog(t *testing.T) {
	products := inputIngredientMap([]ai.InputIngredient{
		{ProductID: "food", VariantID: "food-upc", Description: "Catalog food", Size: "1 lb", AisleNumber: " 2 "},
		{ProductID: "wine", VariantID: "wine-upc", Description: "Catalog wine", Size: "750mL", AisleNumber: "Wine"},
	})
	items := []ai.ShoppingItem{{ProductID: "food", Name: "AI food", Needed: "2 lb", Buy: new(float64(2))}, {ProductID: "wine", Name: "AI wine", Needed: "1 bottle"}}
	prepared := prepareShoppingItems(items, products)
	require.Len(t, prepared, 2)
	assert.Equal(t, "food-upc", prepared[0].VariantID)
	assert.Equal(t, "Catalog food", prepared[0].Name)
	assert.Equal(t, "1 lb", prepared[0].Size)
	assert.Equal(t, "2", prepared[0].Aisle)
	assert.Equal(t, "2 lb", prepared[0].Needed)
	assert.Equal(t, float64(2), *prepared[0].Buy)
	assert.Equal(t, "wine-upc", prepared[1].VariantID)
	assert.Equal(t, "Catalog wine", prepared[1].Name)
	assert.Equal(t, "750mL", prepared[1].Size)
	assert.Equal(t, "1 bottle", prepared[1].Needed)
	assert.Equal(t, "AI food", items[0].Name, "preparation must not mutate merger output")
}

func TestShoppingViewReceivesPreparedPurchases(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner"}
	p.Saved = []ai.Recipe{recipe}
	purchases := []shoppingPurchaseGroup{{Aisle: "Produce", Items: []shoppingPurchaseItem{{ProductID: "food", Name: "Prepared food", Buy: new(float64(1)), VariantID: "selected-upc", Size: "1 lb"}}}}
	view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{
		params: p, list: ai.ShoppingList{Recipes: p.Saved}, purchases: purchases,
		wineRecommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {Wines: []ai.Ingredient{{ProductID: "wine", Name: "Wine", Quantity: "1 bottle"}}}},
	})
	require.NoError(t, err)
	assert.Equal(t, purchases, view.Purchases, "view must not append wine or enrich shopping items")
}

func TestFinalizedShoppingInputsCombinesCatalogsBeforeQuantityMerge(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "food", Name: "AI food", Quantity: "2 lb"}}}
	p.Saved = []ai.Recipe{recipe}
	require.NoError(t, s.SaveParams(t.Context(), p))
	food := ai.InputIngredient{ProductID: "food", VariantID: "food-upc", Description: "Catalog food", Size: "1 lb"}
	wine := ai.InputIngredient{ProductID: "wine", VariantID: "wine-upc", Description: "Catalog wine", Size: "750mL"}
	require.NoError(t, s.SaveIngredients(t.Context(), p.LocationHash(), []ai.InputIngredient{food}))
	require.NoError(t, s.SaveIngredients(t.Context(), wineStylesCacheKey(recipe.WineStyles, p.Location.ID, p.Date), []ai.InputIngredient{wine}))
	require.NoError(t, s.saveWine(t.Context(), recipe.ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Picked wine", Quantity: "750mL"}}}))
	ingredients, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{food, wine}, catalog)
	assert.Equal(t, append(recipe.Ingredients, ai.Ingredient{ProductID: "wine", Name: "Picked wine", Quantity: "1 bottle"}), ingredients)
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, got []ai.Ingredient, matched []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		assert.Equal(t, ingredients, got)
		assert.Equal(t, []ai.InputIngredient{food, wine}, matched)
		return []ai.ShoppingItem{{ProductID: "food", Name: "AI food", Needed: "2 lb", Buy: new(float64(2))}, {ProductID: "wine", Name: "Picked wine", Needed: "1 bottle", Buy: new(float64(1))}}, nil
	})
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), ingredients, catalog))
	purchases, err := s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	require.Len(t, purchases, 1)
	require.Len(t, purchases[0].Items, 2)
	assert.Equal(t, "Catalog food", purchases[0].Items[0].Name)
	assert.Equal(t, "food-upc", purchases[0].Items[0].VariantID)
}

func TestShoppingCatalogRejectsConflictingSavedVariants(t *testing.T) {
	_, err := shoppingCatalogLookup([]ai.InputIngredient{{ProductID: "wine", VariantID: "first"}, {ProductID: "wine", VariantID: "second"}})
	require.ErrorContains(t, err, "conflicting selected variants")
}

func TestMatchingShoppingCatalog(t *testing.T) {
	needs := []ai.Ingredient{{ProductID: "onion", Name: "Onion", Quantity: "4 oz"}, {ProductID: "onion", Name: "Onions", Quantity: "4 oz"}, {Name: "Olive oil", Quantity: "1 tbsp"}, {ProductID: "missing", Name: "Missing", Quantity: "1"}}
	catalog := []ai.InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}, {ProductID: "other-onion", Description: "Onion", Size: "2 lb"}, {ProductID: "oil", Description: "Olive oil"}, {Description: "Unknown product"}}
	got := MatchingShoppingCatalog(needs, catalog)
	assert.Equal(t, []ai.InputIngredient{catalog[0]}, got)
	assert.Len(t, catalog, 4)
	assert.Len(t, needs, 4)
	assert.Empty(t, MatchingShoppingCatalog(nil, catalog))
}
