package kroger

import (
	"encoding/json"
	"fmt"
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
		name, productID string
		buy             *float64
		quantity        int
	}{
		{name: "whole packages", productID: "food", buy: new(float64(3)), quantity: 3},
		{name: "fraction of package", productID: "food", buy: new(float64(0.5)), quantity: 1},
		{name: "round up", productID: "food", buy: new(float64(2.2)), quantity: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Items []cartItem `json:"items"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, []cartItem{{UPC: "food-upc", Quantity: tc.quantity}}, payload.Items)
				return cartResponse(http.StatusNoContent, ""), nil
			})
			saveShopper(t, a, time.Now().Add(time.Hour))
			form := orderForm()
			setOrderField(t, form, "items", []ai.ShoppingItem{{ProductID: tc.productID, VariantID: "food-upc", Name: "Milk", Buy: tc.buy}})
			result := transferResponse(t, a, postOrder(orderMux(a), form))
			want := ordertypes.TransferItem{ProductID: tc.productID, Name: "Milk"}
			want.AcceptedQuantity = tc.quantity
			assert.Equal(t, ordertypes.TransferApplied, result.Status)
			assert.Equal(t, 1, calls)
			assert.Equal(t, []ordertypes.TransferItem{want}, result.Items)
		})
	}
}

func TestKrogerInvalidPackageQuantityDefaultsToOne(t *testing.T) {
	for _, buy := range []float64{0, -1, math.NaN(), math.Inf(1), float64(math.MaxInt32) + 1} {
		calls := 0
		a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
			calls++
			var payload struct {
				Items []cartItem `json:"items"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, []cartItem{{UPC: "food-upc", Quantity: 1}}, payload.Items)
			return cartResponse(http.StatusNoContent, ""), nil
		})
		input := mustOrderRequest(t)
		input.Items = []ai.ShoppingItem{{ProductID: "food", VariantID: "food-upc", Name: "Milk", Buy: &buy}}
		result, err := a.order(t.Context(), "list", input, cartToken{AccessToken: "shopper"})
		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, ordertypes.TransferApplied, result.Status)
		assert.Equal(t, []ordertypes.TransferItem{{ProductID: "food", Name: "Milk", AcceptedQuantity: 1}}, result.Items)
	}
}

func TestKrogerOrderExcludesItemsWithoutBuy(t *testing.T) {
	for _, withPurchase := range []bool{false, true} {
		t.Run(fmt.Sprint(withPurchase), func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Items []cartItem `json:"items"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, []cartItem{{UPC: "food-upc", Quantity: 2}}, payload.Items)
				return cartResponse(http.StatusNoContent, ""), nil
			})
			input := mustOrderRequest(t)
			input.Items = []ai.ShoppingItem{{ProductID: "garlic", Name: "Garlic"}}
			if withPurchase {
				input.Items = append(input.Items, ai.ShoppingItem{ProductID: "food", VariantID: "food-upc", Buy: new(float64(2))})
			}
			result, err := a.order(t.Context(), "list", input, cartToken{AccessToken: "shopper"})
			if withPurchase {
				require.NoError(t, err)
				assert.Equal(t, 1, calls)
				assert.Equal(t, []ordertypes.TransferItem{{ProductID: "food", AcceptedQuantity: 2}}, result.Items)
			} else {
				require.ErrorContains(t, err, "no purchase quantities")
				assert.Zero(t, calls)
			}
		})
	}
}
