package kroger

import (
	"encoding/json"
	"net/http"
	"testing"

	"careme/internal/ai"
	ordertypes "careme/internal/orders/types"
	"careme/internal/providers/kroger/products"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectedCatalogItemUPCReachesCart(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1/products", r.URL.Path)
		assert.Equal(t, "70500874", r.URL.Query().Get("filter.locationId"))
		return cartResponse(http.StatusOK, `{"data":[{"productId":"food","upc":"product-upc","description":"Milk","categories":["Dairy"],"items":[{"itemId":"out-of-stock","size":"1 pint","price":{"regular":1},"inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}},{"itemId":"unknown-first","size":"2 gal","price":{"regular":7}},{"itemId":"selected-half-gallon","size":"0.5 gal","price":{"regular":3},"inventory":{"stockLevel":"HIGH"}},{"itemId":"other-gallon","size":"1 gal","price":{"regular":5},"inventory":{"stockLevel":"LOW"}}]}]}`), nil
	})}
	catalogClient, err := products.NewClientWithResponses(krogerAPIOrigin, products.WithHTTPClient(client))
	require.NoError(t, err)
	rows, err := searchIngredients(t.Context(), catalogClient, "70500874", "milk", []string{"*"}, false, 0)
	require.NoError(t, err)
	catalog := lo.UniqBy(lo.Map(rows, inputIngredientFromKrogerIngredient), func(i ai.InputIngredient) string { return i.ProductID })
	require.Len(t, catalog, 1)
	assert.Equal(t, "selected-half-gallon", catalog[0].VariantID)
	assert.Equal(t, "0.5 gal", catalog[0].Size)
	assert.Equal(t, float32(3), *catalog[0].PriceRegular)
	calls := 0
	a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Items []cartItem `json:"items"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, []cartItem{{UPC: "selected-half-gallon", Quantity: 2}}, payload.Items)
		return cartResponse(http.StatusNoContent, ""), nil
	})
	input := mustOrderRequest(t)
	// One gallon needed, using the selected half-gallon package.
	input.Items = []ai.ShoppingItem{{ProductID: catalog[0].ProductID, VariantID: catalog[0].VariantID, Buy: new(float64(2))}}
	result, err := a.order(t.Context(), "list", input, cartToken{AccessToken: "shopper"})
	require.NoError(t, err)
	assert.Equal(t, ordertypes.TransferApplied, result.Status)
	assert.Equal(t, 2, result.Items[0].AcceptedQuantity)
	assert.Equal(t, 1, calls)
}

func TestKrogerOldSnapshotsRequireSelectedItemUPC(t *testing.T) {
	for _, items := range [][]ai.ShoppingItem{
		{{ProductID: "food"}}, // Old catalog or persisted shopping snapshot.
		{{ProductID: "food", VariantID: "food-upc"}, {ProductID: "wine"}}, // Old wine recommendation.
		{{ProductID: "food", VariantID: " "}},
		{{ProductID: "food", VariantID: "food-upc"}, {ProductID: "food"}}, // Cannot infer the missing selection from another row.
	} {
		a := orderFixture(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("missing identifier must prevent every cart write")
			return nil, nil
		})
		input := mustOrderRequest(t)
		for i := range items {
			items[i].Buy = new(float64(1))
		}
		input.Items = items
		result, err := a.order(t.Context(), "old-list", input, cartToken{AccessToken: "shopper"})
		require.ErrorContains(t, err, "missing selected item UPC")
		assert.ErrorContains(t, err, "rebuild the shopping list")
		assert.Equal(t, ordertypes.TransferFailed, result.Status)
		assert.Empty(t, result.Items)
	}
}
