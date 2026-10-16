package recipes

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestKrogerOrderFormContainsMergedFoodAndSelectedWine(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Kroger.CartTokenKey = "configured"
	p := DefaultParams(&locations.Location{ID: "70500874", Chain: "QFC", Name: "QFC"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "food", Name: "Chicken", Quantity: "2 lbs"}}}
	p.Saved = []ai.Recipe{recipe}
	food := []shoppingPurchaseGroup{{Aisle: "Meat", Items: []shoppingPurchaseItem{{ShoppingItem: ai.ShoppingItem{ProductID: "food", Name: `Chicken & "herbs"`, Needed: "2 lbs", Buy: buyNumber(2)}, Size: "1 lb"}}}}
	wine := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{
		params: p, list: ai.ShoppingList{Recipes: p.Saved}, purchases: food, hash: p.Hash(),
		currentUser:         &utypes.User{ID: "user", Email: []string{"chef@example.test"}},
		wineRecommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {Wines: []ai.Ingredient{wine, {ProductID: "other", Name: "Other wine"}}}},
	})
	require.NoError(t, err)
	require.NoError(t, s.populateKrogerOrderView(&view, httptest.NewRequest("GET", "/recipes", nil)))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
	document, err := html.Parse(strings.NewReader(page.String()))
	require.NoError(t, err)
	values := map[string]string{}
	var inspect func(*html.Node)
	inspect = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			values[attrs["name"]] = attrs["value"]
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
	}
	inspect(document)
	var posted []ai.ShoppingItem
	require.NoError(t, json.Unmarshal([]byte(values["items"]), &posted))
	require.Len(t, posted, 2)
	assert.Equal(t, food[0].Items[0].ShoppingItem, posted[0])
	assert.Equal(t, ai.ShoppingItem{ProductID: wine.ProductID, Name: wine.Name, Needed: wine.Quantity}, posted[1])
	assert.Equal(t, "70500874", values["store_id"])
	assert.Equal(t, "QFC", values["chain"])
	assert.Equal(t, p.Hash(), values["list_key"])
	assert.Contains(t, values["return_url"], "#shopping-list-section")
	assert.Contains(t, page.String(), `action="/kroger/order"`)
	assert.NotContains(t, page.String(), `target="_blank"`)
	assert.NotContains(t, page.String(), "other")
	assert.Len(t, food, 1, "wine is appended without changing cached food")
}

func TestKrogerOrderReturnNoticeAndCartLink(t *testing.T) {
	for _, scenario := range []struct{ query, want, link string }{
		{"kroger_status=complete&kroger_added=2&kroger_unmatched=1&kroger_cart=https://www.qfc.com/cart", "Sent 2 products to your cart.", "https://www.qfc.com/cart"},
		{"kroger_status=uncertain&kroger_cart=https://www.kroger.com/cart", "could not confirm", "https://www.kroger.com/cart"},
		{"kroger_status=changed", "list changed", ""},
		{"kroger_status=denied&kroger_cart=javascript:alert(1)", "was not approved", ""},
		{"kroger_status=complete&kroger_cart=https://evil.test/cart", "products were sent to your cart", ""},
		{"kroger_status=%3Cscript%3Ealert(1)%3C/script%3E&kroger_cart=https://www.qfc.com/cart", "", ""},
	} {
		t.Run(scenario.query, func(t *testing.T) {
			s := newTestServer(t)
			s.cfg.Kroger.CartTokenKey = "configured"
			view := shoppingListPageView{Hash: "hash", Location: locations.Location{ID: "70500874", Chain: "QFC"}}
			require.NoError(t, s.populateKrogerOrderView(&view, httptest.NewRequest("GET", "/recipes?"+scenario.query, nil)))
			assert.Equal(t, scenario.link, view.KrogerOrder.CartURL)
			if scenario.want != "" {
				assert.Contains(t, view.KrogerOrder.Notice, scenario.want)
			} else {
				assert.Empty(t, view.KrogerOrder.Notice)
			}
			var page bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_purchase_section", view))
			assert.NotContains(t, page.String(), "alert(1)")
			assert.NotContains(t, page.String(), "evil.test")
			if strings.Contains(scenario.query, "unmatched=1") {
				assert.Contains(t, page.String(), "1 item could not be matched")
			}
		})
	}
}

func TestKrogerOrderFormOnlyForConfiguredKrogerLists(t *testing.T) {
	for _, scenario := range []struct{ key, store string }{{"", "70500874"}, {"configured", "walmart_123"}} {
		s := newTestServer(t)
		s.cfg.Kroger.CartTokenKey = scenario.key
		view := shoppingListPageView{Location: locations.Location{ID: scenario.store}}
		require.NoError(t, s.populateKrogerOrderView(&view, httptest.NewRequest("GET", "/recipes", nil)))
		assert.Nil(t, view.KrogerOrder)
	}
}
