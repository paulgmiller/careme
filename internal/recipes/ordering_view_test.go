package recipes

import (
	"bytes"
	"testing"

	"careme/internal/ordering"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderingShoppingListLinkDoesNotClaimCartAdditions(t *testing.T) {
	t.Parallel()
	view := shoppingListPageView{OrderingActions: []orderingActionView{{
		Provider: "list-provider", Brand: "List provider", URL: "https://example.test/shopping-list",
		Result: &ordering.Result{Kind: ordering.ShoppingListCreated, Status: "complete", Sent: []ordering.Item{{Name: "Garlic", Needed: "2 cloves"}}},
	}}}
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	assert.Contains(t, page.String(), "Your shopping list is ready.")
	assert.Contains(t, page.String(), "Open shopping list")
	assert.Contains(t, page.String(), `href="https://example.test/shopping-list"`)
	assert.NotContains(t, page.String(), "Products sent")
	assert.NotContains(t, page.String(), "Not sent to your cart")
	assert.NotContains(t, page.String(), "Review List provider cart")
}

func TestOrderingCartReportsActualQuantitiesAndGaps(t *testing.T) {
	t.Parallel()
	view := orderingActionView{
		Provider: "cart-provider", Brand: "Cart provider", URL: "https://example.test/cart", Message: "2 products sent to your cart.",
		Result: &ordering.Result{Kind: ordering.CartUpdated, Status: "complete", Sent: []ordering.Item{
			{Name: "Garlic", PackageQuantity: 1},
			{Name: "Beans", PackageQuantity: 2},
		}, Gaps: []ordering.Item{{Name: "Parsley", Needed: "1 bunch"}}},
	}
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_ordering_result", view))
	assert.Contains(t, page.String(), "Products sent")
	assert.Contains(t, page.String(), "Garlic — 1 package</li>")
	assert.Contains(t, page.String(), "Beans — 2 packages</li>")
	assert.Contains(t, page.String(), "Not sent to your cart")
	assert.Contains(t, page.String(), "Parsley — 1 bunch")
	assert.Contains(t, page.String(), "Review Cart provider cart")
}

func TestOrderingRendersMultipleDestinations(t *testing.T) {
	t.Parallel()
	view := shoppingListPageView{OrderingActions: []orderingActionView{
		{Provider: "cart", ActionURL: "/recipes/list/ordering/cart", ActionLabel: "Add to cart", CanSubmit: true},
		{Provider: "list", ActionURL: "/recipes/list/ordering/list", ActionLabel: "Create shopping list", Description: "Choose your store after opening the list.", CanSubmit: true},
		{Provider: "already-sent", ActionURL: "/recipes/list/ordering/already-sent", ActionLabel: "Send again", CanSubmit: false},
	}}
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	assert.Contains(t, page.String(), `action="/recipes/list/ordering/cart"`)
	assert.Contains(t, page.String(), `action="/recipes/list/ordering/list"`)
	assert.Contains(t, page.String(), `action="/recipes/list/ordering/cart" target="_blank" rel="noopener noreferrer"`)
	assert.Contains(t, page.String(), `action="/recipes/list/ordering/list" target="_blank" rel="noopener noreferrer"`)
	assert.Contains(t, page.String(), "Opens in a new tab.")
	assert.NotContains(t, page.String(), "disabled=true")
	assert.Contains(t, page.String(), "Choose your store after opening the list.")
	assert.NotContains(t, page.String(), "Send again")
	assert.NotContains(t, page.String(), `action="/recipes/list/ordering/already-sent"`)
}
