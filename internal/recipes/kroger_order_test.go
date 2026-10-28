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
	ordertypes "careme/internal/orders/types"
	"careme/internal/providers/kroger"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestKrogerOrderFormContainsMergedFoodAndSelectedWine(t *testing.T) {
	s := newTestServer(t)
	s.orderView = orderViewBuilderFunc(kroger.OrderView)
	p := DefaultParams(&locations.Location{ID: "70500874", Chain: "QFC", Name: "QFC", Address: "123 Store Street", ZipCode: "98004"}, time.Now())
	recipe := ai.Recipe{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "food", Name: "Chicken", Quantity: "2 lbs"}}}
	p.Saved = []ai.Recipe{recipe}
	food := []shoppingPurchaseGroup{{Aisle: "Meat", Items: []shoppingPurchaseItem{{ProductID: "food", Name: `Chicken & "herbs"`, Needed: "2 lbs", Buy: new(float64(2)), Size: "1 lb"}}}}
	wine := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{
		params: p, list: ai.ShoppingList{Recipes: p.Saved}, purchases: food, hash: p.Hash(),
		currentUser:         &utypes.User{ID: "user", Email: []string{"chef@example.test"}},
		wineRecommendations: map[string]*ai.WineSelection{recipe.ComputeHash(): {Wines: []ai.Ingredient{wine, {ProductID: "other", Name: "Other wine"}}}},
	})
	require.NoError(t, err)
	if view.User == nil {
		view.User = &utypes.User{ID: "viewer"}
	}
	require.NoError(t, s.populateOrderView(httptest.NewRequest("GET", "/recipes", nil).Context(), &view))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", view))
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
	var posted ordertypes.TransferRequest
	require.NoError(t, json.Unmarshal([]byte(values["transfer_request"]), &posted))
	require.Len(t, posted.Items, 2)
	assert.Equal(t, food[0].Items[0].ShoppingItem, posted.Items[0])
	assert.Equal(t, ai.ShoppingItem{ProductID: wine.ProductID, Name: wine.Name, Needed: wine.Quantity}, posted.Items[1])
	assert.Equal(t, *p.Location, posted.Location)
	assert.NotContains(t, values, "items")
	assert.NotContains(t, values, "location")
	assert.NotContains(t, values, "return_url")
	assert.NotContains(t, values, "store_id")
	assert.NotContains(t, values, "chain")
	assert.NotContains(t, values, "list_key")
	assert.Equal(t, p.Hash(), values["idempotency_key"])
	assert.Contains(t, posted.ReturnURL, "#shopping-list-section")
	assert.Contains(t, page.String(), `action="/kroger/order-transfers"`)
	assert.NotContains(t, page.String(), `target="_blank"`)
	assert.NotContains(t, page.String(), "other")
	assert.Len(t, food, 1, "wine is appended without changing cached food")
}
