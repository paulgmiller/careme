package kroger

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"careme/internal/ai"
	ordertypes "careme/internal/orders/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKrogerOrderPackageQuantities(t *testing.T) {
	for _, tc := range []struct {
		name, productID, reason string
		buy                     *float64
		quantity                int
	}{
		{name: "default", productID: "food", quantity: 1},
		{name: "whole packages", productID: "food", buy: new(float64(3)), quantity: 3},
		{name: "fraction of package", productID: "food", buy: new(float64(0.5)), quantity: 1},
		{name: "round up", productID: "food", buy: new(float64(2.2)), quantity: 3},
		{name: "unavailable", productID: "missing", buy: new(float64(3.2)), quantity: 4, reason: "product_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Items []CartItem `json:"items"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: tc.quantity}}, payload.Items)
				return cartResponse(http.StatusNoContent, ""), nil
			})
			saveShopper(t, a, time.Now().Add(time.Hour))
			form := orderForm()
			setOrderField(t, form, "items", []ai.ShoppingItem{{ProductID: tc.productID, Name: "Milk", Buy: tc.buy}})
			result := transferResponse(t, a, postOrder(orderMux(a), form))
			want := ordertypes.TransferItem{ProductID: tc.productID, Name: "Milk"}
			if tc.reason == "" {
				want.AcceptedQuantity = tc.quantity
				assert.Equal(t, ordertypes.TransferApplied, result.Status)
				assert.Equal(t, 1, calls)
			} else {
				want.RejectedQuantity, want.RejectionReason = tc.quantity, tc.reason
				assert.Equal(t, ordertypes.TransferFailed, result.Status)
				assert.Zero(t, calls)
			}
			assert.Equal(t, []ordertypes.TransferItem{want}, result.Items)
		})
	}
}

func TestKrogerInvalidPackageQuantityDoesNotWriteCart(t *testing.T) {
	for _, buy := range []float64{0, -1, math.NaN(), math.Inf(1), float64(math.MaxInt32) + 1} {
		a := orderFixture(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid quantity must not write the cart")
			return nil, nil
		})
		input := mustOrderRequest(t)
		input.Items = []ai.ShoppingItem{{ProductID: "food", Name: "Milk", Buy: &buy}}
		result, err := a.order(t.Context(), "list", input, CartToken{AccessToken: "shopper"})
		require.ErrorContains(t, err, "invalid Kroger package quantity")
		assert.Equal(t, ordertypes.TransferFailed, result.Status)
	}
}
