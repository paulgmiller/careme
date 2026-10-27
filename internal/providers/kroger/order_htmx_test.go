package kroger

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func htmxOrderRequest(handler http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Idempotency-Key", "list-hash")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCartTransferHTMXRendersSavedStatus(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	mux := orderMux(a)
	path := "/kroger/cart-transfers/" + ordertypes.TransferID("list-hash")
	empty := htmxOrderRequest(mux, http.MethodGet, path, nil)
	assert.Equal(t, http.StatusNoContent, empty.Code)
	assert.Empty(t, empty.Body.String())
	assert.Equal(t, http.StatusNotFound, getTransfer(mux, ordertypes.TransferID("list-hash")).Code)
	posted := htmxOrderRequest(mux, http.MethodPost, "/kroger/cart-transfers", orderForm())
	require.Equal(t, http.StatusOK, posted.Code)
	assert.Equal(t, "text/html; charset=utf-8", posted.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", posted.Header().Get("Cache-Control"))
	assert.Equal(t, "HX-Request", posted.Header().Get("Vary"))
	assert.Contains(t, posted.Body.String(), "Sent 2 products to your cart")
	assert.Contains(t, posted.Body.String(), `href="https://www.qfc.com/cart" target="_blank"`)
	assert.NotContains(t, posted.Body.String(), `type="submit"`)
	assert.NotContains(t, posted.Body.String(), `hx-trigger="load delay:5s"`)
	reloaded := htmxOrderRequest(mux, http.MethodGet, path, nil)
	assert.Equal(t, posted.Body.String(), reloaded.Body.String())
	result := transferResponse(t, getTransfer(mux, ordertypes.TransferID("list-hash")))
	assert.Equal(t, "applied", result.Status)
	assert.Equal(t, 1, calls)
}

func TestCartTransferHTMXLoginRemainsProviderOwned(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must connect first"); return nil, nil })
	mux := orderMux(a)
	path := "/kroger/cart-transfers/" + ordertypes.TransferID("list-hash")
	posted := htmxOrderRequest(mux, http.MethodPost, "/kroger/cart-transfers", orderForm())
	assert.Equal(t, http.StatusNoContent, posted.Code)
	assert.Equal(t, path+"/connect", posted.Header().Get("HX-Redirect"))
	pending := htmxOrderRequest(mux, http.MethodGet, path, nil)
	assert.Equal(t, http.StatusOK, pending.Code)
	assert.Contains(t, pending.Body.String(), "Connect account")
	assert.NotContains(t, pending.Body.String(), `target="_blank"`)
	assert.NotContains(t, pending.Body.String(), `type="submit"`)
	assert.Empty(t, pending.Header().Get("HX-Redirect"), "loading a saved transfer does not start OAuth")
}

func TestCartTransferHTMXPollingAndRetryPresentation(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, status := range []string{"pending", "uncertain", "lookup_failed"} {
		t.Run(status, func(t *testing.T) {
			a := newTestOrderHandler(t, &CartClient{})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/kroger/cart-transfers/transfer", nil)
			request.Header.Set("HX-Request", "true")
			transfer := cartTransfer{IdempotencyKey: "list", Request: ordertypes.TransferRequest{Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}}}
			transfer.Result = transfer.result(status, 0, 0)
			a.writeTransferView(response, request, transfer)
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, status == "pending", strings.Contains(response.Body.String(), `hx-trigger="load delay:5s"`))
			assert.Equal(t, status == "lookup_failed", strings.Contains(response.Body.String(), `type="submit"`))
		})
	}
}

func TestCartTransferHTMXSignInReturnsToShoppingPage(t *testing.T) {
	a := newTestOrderHandler(t, &CartClient{})
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	for _, current := range []string{"https://careme.test/recipes?h=list", "https://evil.test/recipes?h=list"} {
		request := httptest.NewRequest(http.MethodGet, "https://careme.test/kroger/cart-transfers/id", nil)
		request.Header.Set("HX-Request", "true")
		request.Header.Set("HX-Current-URL", current)
		response := httptest.NewRecorder()
		orderMux(a).ServeHTTP(response, request)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
		redirect, err := url.Parse(response.Header().Get("HX-Redirect"))
		require.NoError(t, err)
		destination, err := base64.RawURLEncoding.DecodeString(redirect.Query().Get("return_to_b64"))
		require.NoError(t, err)
		if strings.HasPrefix(current, "https://careme.test/") {
			assert.Equal(t, "/recipes?h=list", string(destination))
		} else {
			assert.Equal(t, "/kroger/cart-transfers/id", string(destination))
		}
	}
}
