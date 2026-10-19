package kroger

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/cache"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKrogerOrderReturnNoticeAndCartLink(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, scenario := range []struct{ query, want, link string }{
		{"kroger_status=complete&kroger_added=2&kroger_unmatched=1&kroger_cart=https://www.qfc.com/cart", "Sent 2 products to your cart.", "https://www.qfc.com/cart"},
		{"kroger_status=uncertain&kroger_cart=https://www.kroger.com/cart", "could not confirm", "https://www.kroger.com/cart"},
		{"kroger_status=complete&kroger_cart=https://www.fredmeyer.com/cart", "products were sent to your cart", "https://www.fredmeyer.com/cart"},
		{"kroger_status=complete&kroger_cart=https://www.fredmeyer.com.evil.test/cart", "products were sent to your cart", ""},
		{"kroger_status=changed", "list changed", ""},
		{"kroger_status=denied&kroger_cart=javascript:alert(1)", "was not approved", ""},
		{"kroger_status=complete&kroger_cart=https://evil.test/cart", "products were sent to your cart", ""},
		{"kroger_status=%3Cscript%3Ealert(1)%3C/script%3E&kroger_cart=https://www.qfc.com/cart", "", ""},
	} {
		t.Run(scenario.query, func(t *testing.T) {
			input := ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}, Query: httptest.NewRequest("GET", "/recipes?"+scenario.query, nil).URL.Query()}
			order, err := OrderView(input)
			require.NoError(t, err)
			view := map[string]any{"Order": order, "Purchases": nil, "HasSavedRecipes": true, "Hash": "list"}
			assert.Equal(t, scenario.link, order.ReviewURL)
			if scenario.want != "" {
				assert.Contains(t, order.Notice, scenario.want)
			} else {
				assert.Empty(t, order.Notice)
			}
			var page bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", view))
			if input.Query.Get("kroger_status") == "complete" {
				assert.NotContains(t, page.String(), `action="/kroger/order"`)
				assert.NotContains(t, page.String(), "Add to QFC cart")
				assert.NotContains(t, page.String(), "Build Shopping List")
			} else {
				assert.Contains(t, page.String(), `action="/kroger/order"`)
			}
			assert.NotContains(t, page.String(), "alert(1)")
			assert.NotContains(t, page.String(), "evil.test")
			if scenario.link != "" {
				assert.Contains(t, page.String(), `href="`+scenario.link+`"`)
				assert.Contains(t, page.String(), "Review cart (opens a new tab)")
			}
			if strings.Contains(scenario.query, "unmatched=1") {
				assert.Contains(t, page.String(), "1 item could not be matched")
			}
		})
	}
}

func TestKrogerOrderViewRejectsInvalidItems(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Items: []ai.ShoppingItem{{Name: "Invalid", Buy: &invalid}}})
	require.ErrorContains(t, err, "encode shopping items for Kroger")
}

func TestKrogerOrderViewForm(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, chain := range []string{"QFC", ""} {
		view, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: chain}, ListKey: "list", ReturnURL: "/recipes?h=list"})
		require.NoError(t, err)
		assert.Equal(t, "/kroger/order", view.ActionURL)
		brand := chain
		if brand == "" {
			brand = "Kroger"
		}
		assert.Equal(t, "Add to "+brand+" cart", view.ActionLabel)
		var location locationtypes.Location
		require.NoError(t, json.Unmarshal([]byte(view.Fields.Location), &location))
		assert.Equal(t, "70500874", location.ID)
		assert.Equal(t, chain, location.Chain)
		assert.Equal(t, "list", view.Fields.ListKey)
		assert.Equal(t, "/recipes?h=list", view.Fields.ReturnURL)
	}
}

func TestKrogerOrderViewRejectsInvalidLocation(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{Lat: &invalid}})
	require.ErrorContains(t, err, "encode ordering location")
}

func TestKrogerOrderViewAcceptsEveryChainCart(t *testing.T) {
	for banner, domain := range chainDomains {
		t.Run(banner, func(t *testing.T) {
			destination := "https://www." + domain + "/cart"
			view, err := OrderView(ordertypes.OrderViewInput{Query: httptest.NewRequest("GET", "/recipes?kroger_status=complete&kroger_cart="+destination, nil).URL.Query()})
			require.NoError(t, err)
			assert.Equal(t, destination, view.ReviewURL)
			assert.Equal(t, destination, cartURL(banner))
		})
	}
}

func TestOrderHandlerViewUsesTransfersWithoutReturnQuery(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, status := range []string{"complete", "uncertain", "pending"} {
		t.Run(status, func(t *testing.T) {
			handler := newTestOrderHandler(t, &CartClient{})
			store := handler.cache
			input := ordertypes.OrderViewInput{UserID: "chef", ListKey: "list", Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}}
			require.NoError(t, store.Put(t.Context(), transferKey("chef", "list", 0), `{"status":"`+status+`","added":2}`, cache.Unconditional()))
			view, err := handler.OrderView(t.Context(), input)
			require.NoError(t, err)
			assert.Equal(t, "https://www.qfc.com/cart", view.ReviewURL)
			var page bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", view))
			assert.NotContains(t, page.String(), "Add to QFC cart")
			if status == "pending" {
				assert.Empty(t, view.ActionURL)
				assert.NotContains(t, page.String(), "Add again")
			} else {
				assert.True(t, view.Secondary)
				assert.Contains(t, page.String(), "Add again")
				assert.Contains(t, page.String(), `name="attempt" value="1"`)
			}
			input.UserID = "other-chef"
			other, err := handler.OrderView(t.Context(), input)
			require.NoError(t, err)
			assert.Equal(t, "Add to QFC cart", other.ActionLabel)
			input.UserID, input.ListKey = "chef", "other-list"
			other, err = handler.OrderView(t.Context(), input)
			require.NoError(t, err)
			assert.Equal(t, "Add to QFC cart", other.ActionLabel)
		})
	}
}

func TestOrderHandlerViewUsesLatestAttemptAndPreservesFailures(t *testing.T) {
	handler := newTestOrderHandler(t, &CartClient{})
	store := handler.cache
	input := ordertypes.OrderViewInput{UserID: "chef", ListKey: "list", Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}}
	require.NoError(t, store.Put(t.Context(), transferKey("chef", "list", 0), `{"status":"complete"}`, cache.Unconditional()))
	require.NoError(t, store.Put(t.Context(), transferKey("chef", "list", 1), `{"status":"complete","added":3,"gaps":[{"name":"Salt"}]}`, cache.Unconditional()))
	view, err := handler.OrderView(t.Context(), input)
	require.NoError(t, err)
	assert.Equal(t, "2", view.Fields.Attempt)
	assert.Contains(t, view.Notice, "Sent 3 products")
	assert.Contains(t, view.Notice, "1 item could not be matched")
	input.Query = url.Values{"kroger_status": {"lookup_failed"}}
	view, err = handler.OrderView(t.Context(), input)
	require.NoError(t, err)
	assert.Contains(t, view.Notice, "Could not check Kroger products")
	require.NoError(t, store.Put(t.Context(), transferKey("chef", "list", 1), `broken`, cache.Unconditional()))
	_, err = handler.OrderView(t.Context(), input)
	require.ErrorContains(t, err, "load Kroger transfer")
}
