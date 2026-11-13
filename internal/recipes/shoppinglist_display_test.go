package recipes

import (
	"bytes"
	"testing"
	"time"

	"careme/internal/locations"
	"careme/internal/templates"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShoppingViewSeparatesPantryFromCatalogItemsInSavedGroups(t *testing.T) {
	_ = newTestServer(t)
	sardines := shoppingPurchaseItem{ProductID: "101", Name: "Sardines", Buy: new(float64(2)), Size: "4.4 oz"}
	garlic := shoppingPurchaseItem{ProductID: "202", Name: "Garlic", Needed: "6 cloves"}
	breadcrumbs := shoppingPurchaseItem{Name: "Breadcrumbs", Needed: "¼ cup"}
	onions := shoppingPurchaseItem{ProductID: "303", Name: "Onions", Buy: new(float64(1))}
	groups := []shoppingPurchaseGroup{{Aisle: "Other items", Items: []shoppingPurchaseItem{sardines, garlic, breadcrumbs}}, {Aisle: "Produce", Items: []shoppingPurchaseItem{onions}}}
	p := DefaultParams(&locations.Location{ID: "mnfoodclub_delivery"}, time.Now())
	view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: p, purchases: groups})
	require.NoError(t, err)
	assert.Equal(t, []shoppingPurchaseGroup{{Aisle: "To buy", Items: []shoppingPurchaseItem{sardines, garlic}}, {Aisle: "Produce", Items: []shoppingPurchaseItem{onions}}, {Aisle: "Have at home", Items: []shoppingPurchaseItem{breadcrumbs}}}, view.Purchases)
	assert.Len(t, groups[0].Items, 3, "saved groups must remain unchanged")
	var body bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_purchase_section", view))
	assert.Contains(t, body.String(), ">To buy</h3>")
	assert.Contains(t, body.String(), ">Have at home</h3>")
	assert.Contains(t, body.String(), "6 cloves")
	assert.Contains(t, body.String(), "¼ cup")
	assert.NotContains(t, body.String(), "Other items")
}

func TestShoppingViewKeepsProduceWithoutConversionToBuy(t *testing.T) {
	leek := shoppingPurchaseItem{ProductID: "fullcircle_produce_box_king_richard_leek", Name: "Produce box King Richard Leek", Needed: "8 ounces trimmed white and light-green parts", Size: "1 bunch"}
	kale := shoppingPurchaseItem{ProductID: "fullcircle_produce_box_green_kale", Name: "Produce box Green Kale", Needed: "8 ounces, stems removed", Size: "1 bunch"}
	oil := shoppingPurchaseItem{Name: "Olive oil", Needed: "3 tablespoons"}
	// Older snapshots may already have the misleading heading.
	groups := []shoppingPurchaseGroup{{Aisle: "Have at home", Items: []shoppingPurchaseItem{leek, kale, oil}}}
	got := shoppingPurchaseGroupsForDisplay(groups)
	require.Equal(t, []shoppingPurchaseGroup{
		{Aisle: "To buy", Items: []shoppingPurchaseItem{leek, kale}},
		{Aisle: "Have at home", Items: []shoppingPurchaseItem{oil}},
	}, got)
	var body bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_purchase_section", shoppingListPageView{Purchases: got}))
	toBuy, atHome, found := bytes.Cut(body.Bytes(), []byte(">Have at home</h3>"))
	require.True(t, found)
	assert.Contains(t, string(toBuy), leek.Name)
	assert.Contains(t, string(toBuy), leek.Needed)
	assert.Contains(t, string(toBuy), kale.Name)
	assert.NotContains(t, string(toBuy), "Buy 1")
	assert.NotContains(t, string(atHome), "Produce box")
	assert.Contains(t, string(atHome), oil.Name)
}
