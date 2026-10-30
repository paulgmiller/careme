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

func TestPrepareShoppingPurchasesWithWine(t *testing.T) {
	recipe := ai.Recipe{Title: "Dinner"}
	otherRecipe := ai.Recipe{Title: "Unsaved dinner"}
	wine := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	food := []shoppingPurchaseGroup{{Aisle: "Other items", Items: []shoppingPurchaseItem{{Name: "Pork", Needed: "24 oz"}}}}
	for _, tc := range []struct {
		name            string
		purchases       []shoppingPurchaseGroup
		recommendations map[string]*ai.WineSelection
		wantWine        bool
	}{
		{name: "late pick", purchases: food, recommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {Wines: []ai.Ingredient{wine, {Name: "Backup wine"}}}}, wantWine: true},
		{name: "missing pick", purchases: food},
		{name: "empty pick", purchases: food, recommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {}}},
		{name: "unsaved recipe", purchases: food, recommendations: map[string]*ai.WineSelection{otherRecipe.ComputeHash(): {Wines: []ai.Ingredient{wine}}}},
		{name: "not finalized", recommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {Wines: []ai.Ingredient{wine}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prepareShoppingPurchases(tc.purchases, []ai.Recipe{recipe}, tc.recommendations, nil)
			require.NoError(t, err)
			if !tc.wantWine {
				assert.Equal(t, tc.purchases, got)
				return
			}
			require.Len(t, got, 2)
			assert.Equal(t, food[0], got[0])
			assert.Equal(t, "Wine", got[1].Aisle)
			require.Len(t, got[1].Items, 1)
			assert.Equal(t, ai.ShoppingItem{ProductID: wine.ProductID, Name: wine.Name, Needed: wine.Quantity}, got[1].Items[0].ShoppingItem)
			assert.Equal(t, food, tc.purchases, "rendering must preserve the cached food groups")
		})
	}
}

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
	purchases := []shoppingPurchaseGroup{{Aisle: "Produce", Items: []shoppingPurchaseItem{{ProductID: "food", Name: "Prepared food", VariantID: "selected-upc", Size: "1 lb"}}}}
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
	require.NoError(t, s.saveWineIngredients(t.Context(), recipe.ComputeHash(), []ai.InputIngredient{wine}))
	ingredients, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{food, wine}, catalog)
	assert.Equal(t, recipe.Ingredients, ingredients, "wine quantities remain outside the food merger")
	s.shoppingMerger = shoppingMergerFunc(func(_ context.Context, got []ai.Ingredient, matched []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		assert.Equal(t, ingredients, got)
		assert.Equal(t, []ai.InputIngredient{food}, matched)
		return []ai.ShoppingItem{{ProductID: "food", Name: "AI food", Needed: "2 lb", Buy: new(float64(2))}}, nil
	})
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), ingredients, catalog))
	purchases, err := s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	require.Len(t, purchases, 1)
	require.Len(t, purchases[0].Items, 1)
	assert.Equal(t, "Catalog food", purchases[0].Items[0].Name)
	assert.Equal(t, "food-upc", purchases[0].Items[0].VariantID)
}

func TestShoppingCatalogRejectsConflictingSavedVariants(t *testing.T) {
	_, err := shoppingCatalogLookup([]ai.InputIngredient{{ProductID: "wine", VariantID: "first"}, {ProductID: "wine", VariantID: "second"}})
	require.ErrorContains(t, err, "conflicting selected variants")
}
