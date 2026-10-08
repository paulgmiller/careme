package recipes

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/providers/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type krogerTestTransport func(*http.Request) (*http.Response, error)

func (f krogerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func krogerTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestKrogerCartTransferAndEncryptedConnection(t *testing.T) {
	s := newTestServer(t)
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	s.krogerCartKey = key
	token := kroger.CartToken{AccessToken: "shopper-secret", RefreshToken: "refresh-secret", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, s.saveCartToken(t.Context(), "shopper", token))
	loaded, err := s.loadCartToken(t.Context(), "shopper")
	require.NoError(t, err)
	assert.Equal(t, token.AccessToken, loaded.AccessToken)
	assert.Equal(t, token.RefreshToken, loaded.RefreshToken)
	assert.True(t, token.ExpiresAt.Equal(loaded.ExpiresAt))
	reader, err := s.Cache.Get(t.Context(), s.cartTokenKey("shopper"))
	require.NoError(t, err)
	stored, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NotContains(t, string(stored), "shopper-secret")
	assert.NotContains(t, string(stored), "refresh-secret")
	assert.NotEmpty(t, stored)

	var cartRequests int
	httpClient := &http.Client{Transport: krogerTestTransport(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/connect/oauth2/token":
			return krogerTestResponse(http.StatusOK, `{"access_token":"catalog","expires_in":3600}`), nil
		case "/v1/products/0001111060903":
			assert.Equal(t, "01400943", req.URL.Query().Get("filter.locationId"))
			return krogerTestResponse(http.StatusOK, `{"data":{"items":[{"itemId":"upc-1"}]}}`), nil
		case "/v1/cart/add":
			cartRequests++
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"items":[{"upc":"upc-1","quantity":1}]}`, string(body))
			return krogerTestResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected Kroger request: %s", req.URL)
			return nil, nil
		}
	})}
	s.krogerCart = &kroger.CartClient{ClientID: "id", ClientSecret: "secret", HTTPClient: httpClient, CatalogToken: kroger.NewKrogerTokenManager("id", "secret", httpClient)}
	p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Garlic dish", Ingredients: []ai.Ingredient{{ProductID: "0001111060903", Name: "Garlic", Quantity: "2 cloves"}, {Name: "Salt", Quantity: "1 tsp"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	rr := httptest.NewRecorder()
	s.performCartTransfer(rr, httptest.NewRequest(http.MethodPost, "/recipes/x/kroger-cart", nil), "shopper", p.Hash(), token)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Salt")
	result, err := s.loadCartTransfer(t.Context(), "shopper", p.Hash())
	require.NoError(t, err)
	assert.Equal(t, "complete", result.Status)
	assert.Equal(t, 1, result.Added)
	assert.Equal(t, 1, cartRequests)
	result.Fingerprint = "older-list"
	require.NoError(t, s.saveCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash(), result, cache.Unconditional()))
	repeatReq := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
	repeatReq.SetPathValue("hash", p.Hash())
	repeat := httptest.NewRecorder()
	s.handleKrogerCart(repeat, repeatReq)
	assert.Equal(t, http.StatusOK, repeat.Code)
	assert.Contains(t, repeat.Body.String(), "shopping list changed")
	assert.Equal(t, 1, cartRequests)
}

func TestKrogerCallback(t *testing.T) {
	for _, tc := range []struct {
		name         string
		changeState  func(*krogerAuthState)
		cookieValue  string
		omitCookie   bool
		query        string
		tokenStatus  int
		wantStatus   int
		wantMessage  string
		wantExchange bool
	}{
		{name: "connect and transfer", query: "state=nonce&code=code", wantStatus: http.StatusSeeOther, wantExchange: true},
		{name: "missing cookie", omitCookie: true, wantStatus: http.StatusBadRequest, wantMessage: "connection expired"},
		{name: "invalid encoding", cookieValue: "!", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "invalid JSON", cookieValue: base64.RawURLEncoding.EncodeToString([]byte("invalid")), wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "wrong account", changeState: func(state *krogerAuthState) { state.UserID = "other" }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "wrong state", query: "state=other&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "missing shopping list", changeState: func(state *krogerAuthState) { state.Hash = "" }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "missing timestamp", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Time{} }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "expired state", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Now().Add(-11 * time.Minute) }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "future state", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Now().Add(2 * time.Minute) }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "denied", query: "state=nonce&error=access_denied", wantStatus: http.StatusBadRequest, wantMessage: "not approved"},
		{name: "missing code", query: "state=nonce", wantStatus: http.StatusBadRequest, wantMessage: "did not return a code"},
		{name: "exchange failed", query: "state=nonce&code=code", tokenStatus: http.StatusUnauthorized, wantStatus: http.StatusBadGateway, wantMessage: "Unable to connect", wantExchange: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			s.krogerCartKey = make([]byte, 32)
			p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}}}}
			require.NoError(t, s.SaveParams(t.Context(), p))
			var exchanges, additions int
			client := &http.Client{Transport: krogerTestTransport(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/v1/connect/oauth2/token":
					require.NoError(t, req.ParseForm())
					if req.Form.Get("grant_type") == "authorization_code" {
						exchanges++
						assert.Equal(t, "code", req.Form.Get("code"))
						assert.Equal(t, "https://test.careme.cooking/kroger/callback", req.Form.Get("redirect_uri"))
						if tc.tokenStatus != 0 {
							return krogerTestResponse(tc.tokenStatus, ""), nil
						}
						return krogerTestResponse(http.StatusOK, `{"access_token":"shopper","refresh_token":"refresh","expires_in":3600}`), nil
					}
					return krogerTestResponse(http.StatusOK, `{"access_token":"catalog","expires_in":3600}`), nil
				case "/v1/products/garlic":
					return krogerTestResponse(http.StatusOK, `{"data":{"items":[{"itemId":"garlic-upc"}]}}`), nil
				case "/v1/cart/add":
					additions++
					assert.Equal(t, "Bearer shopper", req.Header.Get("Authorization"))
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					assert.JSONEq(t, `{"items":[{"upc":"garlic-upc","quantity":1}]}`, string(body))
					return krogerTestResponse(http.StatusNoContent, ""), nil
				default:
					t.Fatalf("unexpected Kroger request: %s", req.URL)
					return nil, nil
				}
			})}
			s.krogerCart = &kroger.CartClient{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://test.careme.cooking/kroger/callback", HTTPClient: client, CatalogToken: kroger.NewKrogerTokenManager("id", "secret", client)}
			state := krogerAuthState{Nonce: "nonce", Hash: p.Hash(), UserID: "mock-clerk-user-id", IssuedAt: time.Now()}
			if tc.changeState != nil {
				tc.changeState(&state)
			}
			body, err := json.Marshal(state)
			require.NoError(t, err)
			value := base64.RawURLEncoding.EncodeToString(body)
			if tc.cookieValue != "" {
				value = tc.cookieValue
			}
			req := httptest.NewRequest(http.MethodGet, "/kroger/callback?"+tc.query, nil)
			if !tc.omitCookie {
				req.AddCookie(&http.Cookie{Name: "careme_kroger_state", Value: value})
			}
			rr := httptest.NewRecorder()
			s.handleKrogerCallback(rr, req)
			assert.Equal(t, tc.wantStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tc.wantMessage)
			assert.Equal(t, tc.wantExchange, exchanges == 1)
			if tc.wantStatus == http.StatusSeeOther {
				assert.Equal(t, krogerCartURL, rr.Header().Get("Location"))
				assert.Equal(t, 1, additions)
				token, err := s.loadCartToken(t.Context(), state.UserID)
				require.NoError(t, err)
				assert.Equal(t, "refresh", token.RefreshToken)
			} else {
				assert.Zero(t, additions)
			}
			if !tc.omitCookie {
				cookies := rr.Result().Cookies()
				require.Len(t, cookies, 1)
				assert.Equal(t, -1, cookies[0].MaxAge)
			}
		})
	}
}
