package recipes

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	locationtypes "careme/internal/locations/types"
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
				view.Order = &ordertypes.OrderView{ActionURL: "/destination/order", ActionLabel: "Add to cart", Description: "Review amounts before checkout."}
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

type orderProviderStub struct {
	load  func(context.Context, cache.Cache, locationtypes.Location, string, string) (*ordertypes.OrderTransfer, error)
	build func(ordertypes.OrderViewInput) (*ordertypes.OrderView, error)
}

func (p orderProviderStub) LoadTransfer(ctx context.Context, store cache.Cache, location locationtypes.Location, userID, listKey string) (*ordertypes.OrderTransfer, error) {
	return p.load(ctx, store, location, userID, listKey)
}

func (p orderProviderStub) OrderView(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	return p.build(input)
}

func TestPopulateOrderViewLoadsTransferBeforeFormatting(t *testing.T) {
	for _, failure := range []error{nil, errors.New("transfer cache unavailable")} {
		name := "success"
		if failure != nil {
			name = "load failure"
		}
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			transfer := &ordertypes.OrderTransfer{Status: "complete", Added: 2, NextAttempt: 1}
			view := shoppingListPageView{Hash: "list", Location: locations.Location{ID: "70500874", Chain: "QFC"}}
			built := false
			s.orderView = orderProviderStub{
				load: func(ctx context.Context, store cache.Cache, location locationtypes.Location, userID, listKey string) (*ordertypes.OrderTransfer, error) {
					assert.Same(t, s.Cache, store)
					assert.Equal(t, "mock-clerk-user-id", userID)
					assert.Equal(t, view.Location, location)
					assert.Equal(t, "list", listKey)
					if failure != nil {
						return nil, failure
					}
					return transfer, nil
				},
				build: func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
					built = true
					assert.Same(t, transfer, input.Transfer)
					return &ordertypes.OrderView{ActionLabel: "Add again"}, nil
				},
			}
			err := s.populateOrderView(&view, httptest.NewRequest("GET", "/recipes", nil))
			if failure != nil {
				require.ErrorIs(t, err, failure)
				assert.False(t, built)
				assert.Nil(t, view.Order)
			} else {
				require.NoError(t, err)
				assert.True(t, built)
				require.NotNil(t, view.Order)
				assert.Equal(t, "Add again", view.Order.ActionLabel)
			}
		})
	}
}
