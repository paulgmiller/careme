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

func TestCartTransferBrowserFailurePage(t *testing.T) {
	t.Setenv("CLARITY_PROJECT_ID", "test-project")
	t.Setenv("GOOGLE_TAG_MANAGER_ID", "GTM-TEST")
	for _, code := range []string{"uncertain", "pending", "failed"} {
		t.Run(code, func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("rendering must not write cart"); return nil, nil })
			transfer := cartTransfer{IdempotencyKey: "list", Request: mustOrderRequest(t)}
			transfer.Result = transfer.result(code, 0, 1)
			response := httptest.NewRecorder()
			a.finishTransfer(response, httptest.NewRequest(http.MethodPost, "/kroger/order-transfers", nil), transfer)
			assert.GreaterOrEqual(t, response.Code, 400)
			assert.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Contains(t, response.Body.String(), "<!doctype html>")
			assert.Contains(t, response.Body.String(), transfer.Result.Message)
			assert.Contains(t, response.Body.String(), "Back to shopping list")
			assert.Equal(t, code == "failed", strings.Contains(response.Body.String(), "Try again, chef"))
			assert.NotContains(t, response.Body.String(), "Connect account")
			assert.NotContains(t, response.Body.String(), "hx-post")
			assert.NotContains(t, response.Body.String(), "clarity.ms")
			assert.NotContains(t, response.Body.String(), "googletagmanager.com")
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
