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

func TestShoppingViewSeparatesItemsWithoutBuyInSavedGroups(t *testing.T) {
	_ = newTestServer(t)
	sardines := shoppingPurchaseItem{ProductID: "101", Name: "Sardines", Buy: new(float64(2)), Size: "4.4 oz"}
	garlic := shoppingPurchaseItem{ProductID: "202", Name: "Garlic", Needed: "6 cloves"}
	breadcrumbs := shoppingPurchaseItem{Name: "Breadcrumbs", Needed: "¼ cup"}
	onions := shoppingPurchaseItem{ProductID: "303", Name: "Onions", Buy: new(float64(1))}
	groups := []shoppingPurchaseGroup{{Aisle: "Other items", Items: []shoppingPurchaseItem{sardines, garlic, breadcrumbs}}, {Aisle: "Produce", Items: []shoppingPurchaseItem{onions}}}
	p := DefaultParams(&locations.Location{ID: "mnfoodclub_delivery"}, time.Now())
	view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: p, purchases: groups})
	require.NoError(t, err)
	assert.Equal(t, []shoppingPurchaseGroup{{Aisle: "To buy", Items: []shoppingPurchaseItem{sardines}}, {Aisle: "Produce", Items: []shoppingPurchaseItem{onions}}, {Aisle: "Have at home", Items: []shoppingPurchaseItem{garlic, breadcrumbs}}}, view.Purchases)
	assert.Len(t, groups[0].Items, 3, "saved groups must remain unchanged")
	var body bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_purchase_section", view))
	assert.Contains(t, body.String(), ">To buy</h3>")
	assert.Contains(t, body.String(), ">Have at home</h3>")
	assert.Contains(t, body.String(), "6 cloves")
	assert.Contains(t, body.String(), "¼ cup")
	assert.NotContains(t, body.String(), "Other items")
}
