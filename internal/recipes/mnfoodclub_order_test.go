package recipes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/mnfoodclubstub"
	ordertypes "careme/internal/orders/types"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestFoodClubCartSeparatesManualShopping(t *testing.T) {
	api := httptest.NewServer(mnfoodclubstub.New("https://test.careme.cooking/mnfoodclub-stub"))
	t.Cleanup(api.Close)
	client, err := mnfoodclub.NewOrderClient(config.MNFoodClubConfig{OrderAPIURL: api.URL + "/mnfoodclub-stub", OrderAccessToken: "caremetest-mnfoodclub-stub"}, api.Client())
	require.NoError(t, err)
	c := cache.NewInMemoryCache()
	s := newTestServer(t)
	s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
		return mnfoodclub.OrderViewForUser(t.Context(), c, client, input)
	})
	matched := ai.ShoppingItem{ProductID: "101", VariantID: "1005", Name: "Sardines", Buy: new(float64(2))}
	uncertain := ai.ShoppingItem{ProductID: "202", Name: "Garlic", Needed: "6 cloves"}
	manual := ai.ShoppingItem{Name: `Plain dry breadcrumbs & herbs`, Needed: "¼ cup"}
	view := shoppingListPageView{Hash: "list", User: &utypes.User{ID: "mock-clerk-user-id"}, Location: locations.Location{ID: mnfoodclub.MNFoodClubLocationID, Chain: "MN Food Club"}, Purchases: []shoppingPurchaseGroup{{Items: prepareShoppingItems([]ai.ShoppingItem{matched, manual, uncertain}, map[string]ai.InputIngredient{})}}}
	require.NoError(t, s.populateOrderView(httptest.NewRequest(http.MethodGet, "/recipes?h=list", nil), &view))
	var body bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view.Order))
	assert.Contains(t, body.String(), "Check these separately")
	assert.Contains(t, body.String(), "Plain dry breadcrumbs &amp; herbs: ¼ cup")
	form := url.Values{}
	tokenizer := html.NewTokenizer(strings.NewReader(body.String()))
	for tokenType := tokenizer.Next(); tokenType != html.ErrorToken; tokenType = tokenizer.Next() {
		token := tokenizer.Token()
		if token.Data != "input" {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		form.Set(attrs["name"], attrs["value"])
	}
	var posted ordertypes.TransferRequest
	require.NoError(t, json.Unmarshal([]byte(form.Get("transfer_request")), &posted))
	assert.Equal(t, []ai.ShoppingItem{matched}, posted.Items)
	assert.Len(t, view.Purchases[0].Items, 3, "manual shopping stays on the displayed list")
	mux := http.NewServeMux()
	mnfoodclub.NewOrderHandler(c, auth.DefaultMock(), client).Register(mux)
	r := httptest.NewRequest(http.MethodPost, view.Order.TransferURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, r)
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	require.NoError(t, s.populateOrderView(httptest.NewRequest(http.MethodGet, "/recipes?h=list", nil), &view))
	assert.NotEmpty(t, view.Order.ReviewCartURL)
	body.Reset()
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view.Order))
	assert.Contains(t, body.String(), "Review cart")
	assert.Contains(t, body.String(), "Plain dry breadcrumbs &amp; herbs: ¼ cup")
	assert.NotContains(t, body.String(), "<form")
	assert.Contains(t, body.String(), "Garlic: 6 cloves")
}

func TestOrderViewOnlyManualShoppingHasNoTransferForm(t *testing.T) {
	for _, productID := range []string{"", "  "} {
		t.Run(productID, func(t *testing.T) {
			s := newTestServer(t)
			s.orderView = orderViewBuilderFunc(func(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
				assert.Empty(t, input.Items)
				return &ordertypes.OrderView{TransferURL: "/destination/order", TransferLabel: "Add to cart"}, nil
			})
			view := shoppingListPageView{User: &utypes.User{ID: "viewer"}, Purchases: []shoppingPurchaseGroup{{Items: []shoppingPurchaseItem{{ProductID: productID, Name: "Breadcrumbs", Needed: "¼ cup"}}}}}
			require.NoError(t, s.populateOrderView(httptest.NewRequest(http.MethodGet, "/recipes", nil), &view))
			var body bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view.Order))
			assert.Contains(t, body.String(), "Breadcrumbs: ¼ cup")
			assert.NotContains(t, body.String(), "<form")
		})
	}
}
