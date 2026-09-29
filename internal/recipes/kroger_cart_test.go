package recipes

import (
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"

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
