package mnfoodclubstub

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTestClient struct {
	handler http.Handler
	cookie  *http.Cookie
}

func (c stubTestClient) call(method, path, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://test.careme.cooking/mnfoodclub-stub"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Idempotency-Key", key)
	if method == http.MethodPost && strings.HasPrefix(path, "/handoff/") {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) response {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result
}

const loginRequest = `{"items":[{"product_id":"101","variant_id":"1005","quantity":2},{"product_id":"202"}],"identity_hints":{"user_id":"identity-hints-do-not-login","zip_code":"00501"}}`

func TestLoginResolvesPendingTransfer(t *testing.T) {
	store := newMemoryStore()
	client := stubTestClient{handler: New("https://test.careme.cooking/mnfoodclub-stub", store)}
	first := client.call("POST", "/v1/cart-transfers", "login-key", loginRequest)
	pending := decodeResponse(t, first)
	assert.Equal(t, pendingLogin, pending.Status)
	assert.Empty(t, pending.UserID)
	assert.Empty(t, pending.Items)
	assert.NotContains(t, first.Body.String(), `"user_id"`)
	assert.NotContains(t, first.Body.String(), `"items"`)
	path := "/handoff/" + pending.TransferID
	assert.Equal(t, "https://test.careme.cooking/mnfoodclub-stub"+path, pending.CartURL)
	login := client.call("GET", path, "", "")
	assert.Equal(t, 200, login.Code)
	assert.Contains(t, login.Body.String(), `name="name"`)
	assert.Contains(t, login.Body.String(), "Continue to cart")
	invalid := client.call("POST", path, "", "name=++")
	assert.Equal(t, 400, invalid.Code)
	assert.Empty(t, invalid.Result().Cookies())
	assert.Equal(t, pending, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	name := "Chef Zoë & <herbs>"
	loggedIn := client.call("POST", path, "", url.Values{"name": {" " + name + " "}}.Encode())
	require.Equal(t, 303, loggedIn.Code, loggedIn.Body.String())
	require.Len(t, loggedIn.Result().Cookies(), 1)
	cookie := loggedIn.Result().Cookies()[0]
	assert.Equal(t, "/mnfoodclub-stub/", cookie.Path)
	assert.True(t, cookie.HttpOnly)
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	client.cookie = cookie
	applied := decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", ""))
	assert.Equal(t, "applied", string(applied.Status))
	assert.Equal(t, name, applied.UserID)
	assert.Equal(t, pending.TransferID, applied.TransferID)
	require.Len(t, applied.Items, 2)
	assert.Equal(t, 2, applied.Items[0].AcceptedQuantity)
	assert.Equal(t, 1, applied.Items[1].AcceptedQuantity)
	assert.Equal(t, applied.CartURL, loggedIn.Header().Get("Location"))
	// Replaying POST retains its original pending response; GET is the current status.
	assert.Equal(t, first.Body.String(), client.call("POST", "/v1/cart-transfers", "login-key", loginRequest).Body.String())
	assert.Equal(t, 303, client.call("GET", path, "", "").Code)
	assert.Equal(t, 303, client.call("POST", path, "", "name=Another+Chef").Code)
	assert.Equal(t, applied, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	page := client.call("GET", "/", "", "")
	assert.Contains(t, page.Body.String(), "Signed in as Chef Zoë &amp; &lt;herbs&gt;")
	assert.NotContains(t, page.Body.String(), "<herbs>")
	// Careme's next server request still has no browser cookie. The existing cookie
	// applies its pending transfer as soon as the browser follows the handoff.
	api := stubTestClient{handler: client.handler}
	next := decodeResponse(t, api.call("POST", "/v1/cart-transfers", "next-key", loginRequest))
	assert.Equal(t, pendingLogin, next.Status)
	assert.Equal(t, pendingLogin, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+next.TransferID, "", "")).Status, "API status GET never applies a transfer")
	assert.Equal(t, 303, client.call("GET", "/handoff/"+next.TransferID, "", "").Code)
	assert.Equal(t, name, decodeResponse(t, api.call("GET", "/v1/cart-transfers/"+next.TransferID, "", "")).UserID)
}

func TestLoginFailureDoesNotApplyOrSetCookie(t *testing.T) {
	store := newMemoryStore()
	client := stubTestClient{handler: New("https://test.careme.cooking/mnfoodclub-stub", store)}
	pending := decodeResponse(t, client.call("POST", "/v1/cart-transfers", "failure-key", loginRequest))
	path := "/handoff/" + pending.TransferID
	store.applyErr = errors.New("write failed")
	failed := client.call("POST", path, "", "name=Chef")
	assert.Equal(t, 500, failed.Code)
	assert.Empty(t, failed.Result().Cookies())
	assert.Equal(t, pending, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	for _, method := range []string{"GET", "POST"} {
		assert.Equal(t, 404, client.call(method, "/handoff/unknown", "", "name=Chef").Code)
	}
	store.applyErr = nil
	for _, cookie := range []*http.Cookie{nil, {Name: loginCookieName, Value: "invalid!"}, testNameCookie(" "), testNameCookie(strings.Repeat("x", 101))} {
		client.cookie = cookie
		w := client.call("GET", path, "", "")
		assert.Equal(t, 200, w.Code)
		assert.Contains(t, w.Body.String(), "Food Club test login")
	}
	r := httptest.NewRequest("POST", "https://test.careme.cooking/mnfoodclub-stub"+path, strings.NewReader("name=Chef"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://elsewhere.example")
	w := httptest.NewRecorder()
	client.handler.ServeHTTP(w, r)
	assert.Equal(t, 403, w.Code)
	assert.Empty(t, w.Result().Cookies())
}

func TestLocalLoginCookie(t *testing.T) {
	h := New("http://localhost:8080", newMemoryStore())
	r := httptest.NewRequest("POST", "/v1/cart-transfers", strings.NewReader(loginRequest))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Idempotency-Key", "local")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	pending := decodeResponse(t, w)
	r = httptest.NewRequest("POST", "/handoff/"+pending.TransferID, strings.NewReader("name=Local+Chef"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 303, w.Code)
	require.Len(t, w.Result().Cookies(), 1)
	assert.False(t, w.Result().Cookies()[0].Secure)
	assert.Equal(t, "/", w.Result().Cookies()[0].Path)
}

func TestLoginWithTrailingPublicURLSlash(t *testing.T) {
	client := stubTestClient{handler: New("https://test.careme.cooking/mnfoodclub-stub/", newMemoryStore())}
	pending := decodeResponse(t, client.call("POST", "/v1/cart-transfers", "slash-key", loginRequest))
	form := client.call("GET", "/handoff/"+pending.TransferID, "", "")
	assert.Equal(t, 200, form.Code)
	assert.Contains(t, form.Body.String(), `action="/mnfoodclub-stub/handoff/`)
}
