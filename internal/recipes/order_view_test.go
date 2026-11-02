package recipes

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		{Items: []shoppingPurchaseItem{{ProductID: "food", Name: "Food", Needed: "1 lb", Buy: new(float64(1)), VariantID: "variant-42"}}},
		{Items: []shoppingPurchaseItem{{Name: "Wine", Needed: "1 bottle"}}},
	}}
	s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
		assert.Equal(t, "viewer", input.UserID)
		assert.Equal(t, view.Location, input.Location)
		assert.Equal(t, "list", input.ListKey)
		assert.Equal(t, "/recipes?h=list#shopping-list-section", input.ReturnURL)
		assert.Equal(t, []ai.ShoppingItem{{ProductID: "food", VariantID: "variant-42", Name: "Food", Needed: "1 lb", Buy: new(float64(1))}}, input.Items)
		return &ordertypes.OrderView{IdempotencyKey: "list", TransferURL: "/destination/order", TransferLabel: "Send groceries", TransferRequestJSON: `{"items":[{"name":"Food & Wine"}],"location":{"id":"other-store"},"return_url":"/recipes?h=list"}`}, nil
	})
	if view.User == nil {
		view.User = &utypes.User{ID: "viewer"}
	}
	require.NoError(t, s.populateOrderView(httptest.NewRequest("GET", "/recipes?destination_status=ready", nil), &view))
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
		err := s.populateOrderView(httptest.NewRequest("GET", "/recipes", nil), &view)
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
			} else if scenario.finalized {
				assert.Contains(t, controls.String(), "Shopping list ready.")
				assert.NotContains(t, controls.String(), "Build Shopping List")
			} else {
				assert.Contains(t, controls.String(), "Build Shopping List")
			}
			assert.Contains(t, controls.String(), `aria-label="Share shopping list"`)
		})
	}
}

func TestPopulateOrderViewSearchZIP(t *testing.T) {
	for _, zipCode := range []string{"", "00501", "55401-1234"} {
		t.Run(zipCode, func(t *testing.T) {
			s := newTestServer(t)
			s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
				assert.Equal(t, zipCode, input.ZipCode)
				assert.Equal(t, "99999", input.Location.ZipCode)
				return nil, nil
			})
			view := shoppingListPageView{Location: locations.Location{ZipCode: "99999"}}
			view.User = &utypes.User{ID: "viewer"}
			request := httptest.NewRequest(http.MethodGet, "/recipes", nil)
			if zipCode != "" {
				request.AddCookie(&http.Cookie{Name: locations.ZipCodeCookieName, Value: zipCode})
			}
			require.NoError(t, s.populateOrderView(request, &view))
		})
	}
}

func TestSelectionChangeRequiresRebuildingFinalizedList(t *testing.T) {
	_ = newTestServer(t)
	var controls bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&controls, "shopping_finalize_controls_response", newShoppingFinalizeView("list")))
	assert.Contains(t, controls.String(), "Build Shopping List")
	assert.NotContains(t, controls.String(), "Shopping list ready.")
}

func TestShoppingControlsRefreshOnReturn(t *testing.T) {
	_ = newTestServer(t)
	for _, name := range []string{"shopping_finalize_controls", "shopping_finalize_controls_response"} {
		t.Run(name, func(t *testing.T) {
			var controls bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&controls, name, newShoppingFinalizeView("saved-list")))
			for _, attribute := range []string{
				`hx-get="/recipes?h=saved-list"`,
				`focus from:window`,
				`visibilitychange[document.visibilityState === 'visible'] from:document`,
				`pageshow[event.persisted] from:window`,
				`hx-select="#shopping-finalize-controls"`,
				`hx-swap="outerHTML"`,
				`hx-sync="this:drop"`,
			} {
				assert.Contains(t, controls.String(), attribute)
			}
		})
	}
}

func TestCartHandoffOpensNewTab(t *testing.T) {
	_ = newTestServer(t)
	var form bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&form, "shopping_order_form", ordertypes.OrderView{TransferURL: "/mnfoodclub/order-transfers", TransferLabel: "Add to MN Food Club cart"}))
	assert.Contains(t, form.String(), `target="_blank"`)
	assert.Contains(t, form.String(), `rel="noopener noreferrer"`)
	assert.Contains(t, form.String(), "Add to MN Food Club cart (opens a new tab)")
	assert.Contains(t, form.String(), `hx-boost="false"`)
}

func TestFinalizedCartStateAfterLoginReturn(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "mnfoodclub_delivery", Chain: "MN Food Club"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "101", Name: "Chicken", Quantity: "1 lb"}}}
	original := prepareShoppingFinalize(t, s, p, recipe)
	p.Saved = []ai.Recipe{recipe}
	merges := 0
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		merges++
		return []ai.ShoppingItem{{ProductID: "101", Name: "Chicken", Buy: new(float64(1))}}, nil
	})
	finalized := finalizeShoppingRequest(s, original)
	require.Equal(t, http.StatusOK, finalized.Code, finalized.Body.String())
	target := finalized.Header().Get("HX-Redirect")
	require.Contains(t, target, p.Hash())
	for _, cartURL := range []string{"https://partner.example/handoff/pending", "https://partner.example/cart"} {
		t.Run(cartURL, func(t *testing.T) {
			s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
				assert.Equal(t, p.Hash(), input.ListKey)
				assert.Equal(t, "mock-clerk-user-id", input.UserID)
				assert.Len(t, input.Items, 1)
				return &ordertypes.OrderView{ReviewCartURL: cartURL}, nil
			})
			w := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			request.Header.Set("HX-Request", "true")
			s.handleRecipes(w, request)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), cartURL)
			assert.Contains(t, w.Body.String(), "Review cart (opens a new tab)")
			assert.NotContains(t, w.Body.String(), "Build Shopping List")
		})
	}
	s.orderView = orderViewBuilderFunc(func(ordertypes.OrderViewInput) (*ordertypes.OrderView, error) { return nil, nil })
	w := httptest.NewRecorder()
	s.handleRecipes(w, httptest.NewRequest(http.MethodGet, target, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Shopping list ready.")
	assert.NotContains(t, w.Body.String(), "Build Shopping List")
	assert.Equal(t, 1, merges, "return GETs must reuse the finalized purchases")
}

func TestFinalizedListWithoutSelectionDoesNotAskToBuild(t *testing.T) {
	_ = newTestServer(t)
	var controls bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&controls, "shopping_finalize_controls", shoppingListPageView{Finalized: true}))
	assert.Contains(t, controls.String(), "Shopping list ready.")
	assert.NotContains(t, controls.String(), "Build Shopping List")
	assert.NotContains(t, controls.String(), "Add at least one recipe")
}
