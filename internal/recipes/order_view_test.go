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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPopulateOrderViewUsesDestinationPresentation(t *testing.T) {
	s := newTestServer(t)
	view := shoppingListPageView{Hash: "list", Location: locations.Location{ID: "other-store"}, Purchases: []shoppingPurchaseGroup{
		{Items: []shoppingPurchaseItem{{ShoppingItem: ai.ShoppingItem{Name: "Food", Needed: "1 lb"}}}},
		{Items: []shoppingPurchaseItem{{ShoppingItem: ai.ShoppingItem{Name: "Wine", Needed: "1 bottle"}}}},
	}}
	s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
		assert.Equal(t, view.Location, input.Location)
		assert.Equal(t, "list", input.ListKey)
		assert.Equal(t, "/recipes?h=list#shopping-list-section", input.ReturnURL)
		assert.Equal(t, "ready", input.Query.Get("destination_status"))
		assert.Equal(t, []ai.ShoppingItem{{Name: "Food", Needed: "1 lb"}, {Name: "Wine", Needed: "1 bottle"}}, input.Items)
		return &ordertypes.OrderView{ActionURL: "/destination/order", ActionLabel: "Send groceries", Fields: []ordertypes.OrderField{{Name: "custom_items", Value: `Food & "Wine"`}}, Description: "Destination instructions", Notice: "Ready to review", ReviewURL: "https://destination.example/cart", ReviewLabel: "Review groceries"}, nil
	})
	require.NoError(t, s.populateOrderView(&view, httptest.NewRequest("GET", "/recipes?destination_status=ready", nil)))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	for _, want := range []string{`action="/destination/order"`, `name="custom_items"`, `value="Food &amp; &#34;Wine&#34;"`, "Send groceries", "Destination instructions", "Ready to review", "Review groceries"} {
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
		err := s.populateOrderView(&view, httptest.NewRequest("GET", "/recipes", nil))
		if failure != nil {
			require.ErrorIs(t, err, failure)
		} else {
			require.NoError(t, err)
		}
		assert.Nil(t, view.Order)
	}
}
