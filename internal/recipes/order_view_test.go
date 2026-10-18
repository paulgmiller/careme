package recipes

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"careme/internal/providers/instacart"
	"careme/internal/providers/kroger"

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
		return &ordertypes.OrderView{ActionURL: "/destination/order", ActionLabel: "Send groceries", Fields: ordertypes.OrderFields{Items: `Food & "Wine"`, Location: `{"id":"other-store"}`, ListKey: input.ListKey, ReturnURL: input.ReturnURL}, Description: "Destination instructions", Notice: "Ready to review", ReviewURL: "https://destination.example/cart", ReviewLabel: "Review groceries"}, nil
	})
	require.NoError(t, s.populateOrderView(&view, httptest.NewRequest("GET", "/recipes?destination_status=ready", nil)))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", view))
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	for _, want := range []string{`action="/destination/order"`, `name="items"`, `name="location"`, `name="list_key"`, `name="return_url"`, `value="Food &amp; &#34;Wine&#34;"`, "Send groceries", "Destination instructions", "Ready to review", "Review groceries"} {
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
		assert.Nil(t, view.Orders)
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
				view.Orders = []*ordertypes.OrderView{{ActionURL: "/destination/order", ActionLabel: "Add to cart", Description: "Review amounts before checkout."}}
			}
			var controls bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&controls, "shopping_finalize_controls", view))
			if scenario.ordering {
				assert.Contains(t, controls.String(), `action="/destination/order"`)
				assert.Contains(t, controls.String(), "Add to cart")
				assert.Contains(t, controls.String(), "Review amounts before checkout.")
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

func TestShoppingListRendersBothOrderingDestinations(t *testing.T) {
	s := newTestServer(t)
	s.orderView = orderViewsBuilderFunc(func(input ordertypes.OrderViewInput) ([]*ordertypes.OrderView, error) {
		krogerView, err := kroger.OrderView(input)
		if err != nil {
			return nil, err
		}
		instacartView, err := instacart.OrderView(input)
		if err != nil {
			return nil, err
		}
		return []*ordertypes.OrderView{krogerView, instacartView}, nil
	})
	view := shoppingListPageView{Hash: "list", HasSavedRecipes: true, Finalized: true, Location: locations.Location{ID: "70500874", Chain: "QFC"}, Purchases: []shoppingPurchaseGroup{{Items: []shoppingPurchaseItem{{ShoppingItem: ai.ShoppingItem{Name: `Food & "Wine"`, Needed: "2 cups"}}}}}}
	require.NoError(t, s.populateOrderView(&view, httptest.NewRequest("GET", "/recipes?kroger_status=denied&instacart_status=failed", nil)))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", view))
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	body := page.String()
	assert.Contains(t, body, `action="/kroger/order"`)
	assert.Contains(t, body, `action="/instacart/order"`)
	assert.Contains(t, body, "Add to QFC cart")
	assert.Contains(t, body, "Shop with Instacart")
	assert.Contains(t, body, "Kroger connection was not approved")
	assert.Contains(t, body, "Could not prepare your Instacart list")
	assert.NotContains(t, body, "Build Shopping List")
	assert.Equal(t, 2, strings.Count(body, `name="items"`))
	assert.Less(t, strings.Index(body, `action="/kroger/order"`), strings.Index(body, `action="/instacart/order"`))
	assert.Equal(t, view.Orders[0].Fields, view.Orders[1].Fields)
}

type orderViewsBuilderFunc func(ordertypes.OrderViewInput) ([]*ordertypes.OrderView, error)

func (f orderViewsBuilderFunc) OrderViews(input ordertypes.OrderViewInput) ([]*ordertypes.OrderView, error) {
	return f(input)
}
