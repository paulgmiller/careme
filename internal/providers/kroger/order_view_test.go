package kroger

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

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
			view := map[string]any{"Orders": []*ordertypes.OrderView{order}, "Purchases": nil, "HasSavedRecipes": true, "Hash": "list"}
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
