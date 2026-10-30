package providerregistry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/templates"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoodClubOrderingForAllStorefronts(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/cart-transfers", r.URL.Path)
		assert.Equal(t, "Bearer partner-secret", r.Header.Get("Authorization"))
		var body struct {
			IdentityHints map[string]any `json:"identity_hints"`
			Items         []struct {
				VariantID string `json:"variant_id"`
			} `json:"items"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "00501", body.IdentityHints["zip_code"])
		if assert.Len(t, body.Items, 1) {
			assert.Equal(t, "variant-1005", body.Items[0].VariantID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"transfer_id":"partner-id","status":"applied","user_id":"customer-42","cart_url":"https://shop.foodclub.test/cart","items":[{"product_id":"101","accepted_quantity":1,"rejected_quantity":0}]}`))
	}))
	defer api.Close()
	cfg := &config.Config{MNFoodClub: config.MNFoodClubConfig{OrderAPIURL: api.URL, OrderAccessToken: "partner-secret"}}
	factory := NewFactory(cfg)
	options := orderingTestOptions()
	mux := http.NewServeMux()
	require.NoError(t, factory.RegisterOrders(mux, options))
	require.NoError(t, templates.Init(cfg))
	for _, id := range []string{mnfoodclub.MNFoodClubLocationID, mnfoodclub.TCFarmLocationID, mnfoodclub.NelsonGrassFarmLocationID} {
		t.Run(id, func(t *testing.T) {
			location, err := mnfoodclub.NewLocationBackend().GetLocationByID(t.Context(), id)
			require.NoError(t, err)
			input := ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: id, Location: *location, ZipCode: "00501", Items: []ai.ShoppingItem{{ProductID: "101", VariantID: "variant-1005", Name: "Chicken", Buy: new(float64(1))}}, ReturnURL: "/recipes?h=" + id}
			builder := factory.NewOrderViewBuilder(options.Cache)
			view, err := builder.OrderView(t.Context(), input)
			require.NoError(t, err)
			require.NotNil(t, view)
			assert.Equal(t, "Add to "+location.Chain+" cart", view.TransferLabel)
			var body bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view))
			assert.Contains(t, body.String(), `action="/mnfoodclub/order-transfers"`)
			assert.Contains(t, body.String(), `method="POST"`)
			assert.Contains(t, body.String(), `name="idempotency_key"`)
			assert.NotContains(t, body.String(), "partner-secret")
			var posted ordertypes.TransferRequest
			require.NoError(t, json.Unmarshal([]byte(view.TransferRequestJSON), &posted))
			assert.Equal(t, "00501", posted.ZipCode)
			assert.Equal(t, *location, posted.Location)
			assert.Contains(t, body.String(), "00501")
			form := url.Values{"idempotency_key": {view.IdempotencyKey}, "transfer_request": {view.TransferRequestJSON}}
			r := httptest.NewRequest(http.MethodPost, view.TransferURL, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, r)
			assert.Equal(t, http.StatusSeeOther, response.Code)
			assert.Equal(t, input.ReturnURL, response.Header().Get("Location"))
			view, err = builder.OrderView(t.Context(), input)
			require.NoError(t, err)
			body.Reset()
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view))
			assert.Contains(t, body.String(), "Review cart (opens a new tab)")
			assert.NotContains(t, body.String(), "<form")
		})
	}
	view, err := factory.NewOrderViewBuilder(options.Cache).OrderView(t.Context(), ordertypes.OrderViewInput{UserID: "viewer", Location: locationtypes.Location{ID: "70500874"}})
	require.NoError(t, err)
	assert.Nil(t, view)
}

func TestFoodClubOrderingRequiresSeparateConfiguration(t *testing.T) {
	for _, cfg := range []config.MNFoodClubConfig{
		{OrderAPIURL: "https://orders.foodclub.test"},
		{OrderAccessToken: "partner-token"},
		{OrderAPIURL: "https://orders.foodclub.test?query=1", OrderAccessToken: "partner-token"},
	} {
		require.Error(t, NewFactory(&config.Config{MNFoodClub: cfg}).RegisterOrders(http.NewServeMux(), orderingTestOptions()))
	}
	mux := http.NewServeMux()
	require.NoError(t, NewFactory(&config.Config{MNFoodClub: config.MNFoodClubConfig{APIPath: "https://catalog.test", AccessToken: "catalog-token"}}).RegisterOrders(mux, orderingTestOptions()))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mnfoodclub/order-transfers", nil))
	assert.Equal(t, http.StatusNotFound, response.Code)
}
