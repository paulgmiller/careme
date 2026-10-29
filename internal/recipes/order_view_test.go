package recipes

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPopulateOrderViewUsesDestinationPresentation(t *testing.T) {
	s := newTestServer(t)
	view := shoppingListPageView{Hash: "list", Location: locations.Location{ID: "other-store"}, Purchases: []shoppingPurchaseGroup{
		{Items: []shoppingPurchaseItem{{ProductID: "food", Name: "Food", Needed: "1 lb", VariantID: "variant-42"}}},
		{Items: []shoppingPurchaseItem{{Name: "Wine", Needed: "1 bottle"}}},
	}}
	s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
		assert.Equal(t, "viewer", input.UserID)
		assert.Equal(t, view.Location, input.Location)
		assert.Equal(t, "list", input.ListKey)
		assert.Equal(t, "/recipes?h=list#shopping-list-section", input.ReturnURL)
		assert.Equal(t, []ai.ShoppingItem{{ProductID: "food", VariantID: "variant-42", Name: "Food", Needed: "1 lb"}, {Name: "Wine", Needed: "1 bottle"}}, input.Items)
		return &ordertypes.OrderView{IdempotencyKey: "list", TransferURL: "/destination/order", TransferLabel: "Send groceries", TransferRequestJSON: `{"items":[{"name":"Food & Wine"}],"location":{"id":"other-store"},"return_url":"/recipes?h=list"}`}, nil
	})
	if view.User == nil {
		view.User = &utypes.User{ID: "viewer"}
	}
	require.NoError(t, s.populateOrderView(httptest.NewRequest("GET", "/recipes?destination_status=ready", nil).Context(), &view))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", view))
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	for _, want := range []string{`action="/destination/order"`, `name="transfer_request"`, `name="idempotency_key" value="list"`, `Food &amp; Wine`, "Send groceries"} {
		assert.Contains(t, page.String(), want)
	}
	assert.NotContains(t, page.String(), "Kroger")
	assert.NotContains(t, page.String(), `name="store_id"`)
}

func TestPopulateOrderViewOptionalDestinationAndFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("cannot build destination form")} {
		s := newTestServer(t)
		s.orderView = orderViewBuilderFunc(func(ordertypes.OrderViewInput) (*ordertypes.OrderView, error) { return nil, failure })
		view := shoppingListPageView{}
		view.User = &utypes.User{ID: "viewer"}
		err := s.populateOrderView(httptest.NewRequest("GET", "/recipes", nil).Context(), &view)
		if failure != nil {
			require.ErrorIs(t, err, failure)
		} else {
			require.NoError(t, err)
		}
		assert.Nil(t, view.Order)
	}
}

func TestShoppingListCartReplacesBuildAction(t *testing.T) {
	for _, scenario := range []struct {
		name                string
		finalized, ordering bool
	}{
		{name: "not built"},
		{name: "built with ordering", finalized: true, ordering: true},
		{name: "built without ordering", finalized: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			view := shoppingListPageView{Hash: "list", HasSavedRecipes: true, Finalized: scenario.finalized}
			if scenario.ordering {
				view.Order = &ordertypes.OrderView{TransferURL: "/destination/order", TransferLabel: "Add to cart"}
			}
			var controls bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&controls, "shopping_finalize_controls", view))
			if scenario.ordering {
				assert.Contains(t, controls.String(), `action="/destination/order"`)
				assert.Contains(t, controls.String(), "Add to cart")
				assert.NotContains(t, controls.String(), "Review amounts before checkout.")
				assert.NotContains(t, controls.String(), "Build Shopping List")
				var purchases bytes.Buffer
				require.NoError(t, templates.ShoppingList.ExecuteTemplate(&purchases, "shopping_purchase_section", view))
				assert.NotContains(t, purchases.String(), `action="/destination/order"`, "cart action belongs above the shopping list")
			} else {
				assert.Contains(t, controls.String(), "Build Shopping List")
			}
			assert.Contains(t, controls.String(), `aria-label="Share shopping list"`)
		})
	}
}
