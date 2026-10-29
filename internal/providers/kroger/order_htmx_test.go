package kroger

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCartTransferBrowserLoginRedirect(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must connect first"); return nil, nil })
	response := postOrder(orderMux(a), orderForm())
	require.Equal(t, http.StatusSeeOther, response.Code)
	destination, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https", destination.Scheme)
	assert.NotEmpty(t, destination.Query().Get("state"))
	assert.Empty(t, response.Header().Get("HX-Redirect"))
	assert.NotContains(t, response.Body.String(), "Connect account")
}

func TestCartTransferBrowserReturnsToShoppingList(t *testing.T) {
	for _, status := range []ordertypes.TransferStatus{ordertypes.TransferPending, ordertypes.TransferFailed, ordertypes.TransferApplied} {
		t.Run(string(status), func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("redirecting must not write cart")
				return nil, nil
			})
			transfer := ordertypes.Transfer{TransferID: "list-hash", Status: status, CartURL: "https://www.qfc.com/cart", Items: []ordertypes.TransferItem{{RejectedQuantity: 1, RejectionReason: "missing_product_id"}}}
			require.NoError(t, a.saveJSON(t.Context(), transferKey("mock-clerk-user-id", transfer.TransferID), transfer, cache.Unconditional()))
			response := postOrder(orderMux(a), orderForm())
			assert.Equal(t, http.StatusSeeOther, response.Code)
			assert.Equal(t, mustOrderRequest(t).ReturnURL, response.Header().Get("Location"))
		})
	}
}

func TestCartTransferSignInReturnsToShoppingList(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must sign in first"); return nil, nil })
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	response := postOrder(orderMux(a), orderForm())
	require.Equal(t, http.StatusSeeOther, response.Code)
	destination, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/sign-in", destination.Path)
	returnURL, err := base64.RawURLEncoding.DecodeString(destination.Query().Get("return_to_b64"))
	require.NoError(t, err)
	assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", string(returnURL))
}

func TestCartTransferNativePostRedirectsToCart(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { return cartResponse(http.StatusNoContent, ""), nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	response := postOrder(orderMux(a), orderForm())
	assert.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", response.Header().Get("Location"))
	assert.Empty(t, response.Header().Get("HX-Redirect"))
}
