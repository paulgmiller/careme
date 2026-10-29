package kroger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cartRoundTrip func(*http.Request) (*http.Response, error)

func (f cartRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cartResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}

func TestCartClientConnectAndAdd(t *testing.T) {
	var added []CartItem
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/connect/oauth2/token":
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "grant_type=authorization_code")
			return cartResponse(http.StatusOK, `{"access_token":"shopper-token","refresh_token":"refresh","expires_in":3600}`), nil
		case "/v1/cart/add":
			assert.Equal(t, http.MethodPut, req.Method)
			assert.Equal(t, "Bearer shopper-token", req.Header.Get("Authorization"))
			var body struct {
				Items []CartItem `json:"items"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			added = body.Items
			return cartResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected Kroger request: %s", req.URL)
			return nil, nil
		}
	})}
	cart := CartClient{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://careme.test/kroger/callback", HTTPClient: client}
	destination, err := cart.AuthorizationURL("nonce", "qfc")
	require.NoError(t, err)
	parsed, err := url.Parse(destination)
	require.NoError(t, err)
	assert.Equal(t, "cart.basic:write", parsed.Query().Get("scope"))
	assert.Equal(t, "nonce", parsed.Query().Get("state"))
	assert.Equal(t, "qfc", parsed.Query().Get("banner"))
	token, err := cart.Exchange(context.Background(), "code")
	require.NoError(t, err)
	require.NoError(t, cart.Add(context.Background(), token.AccessToken, []CartItem{{UPC: "0001111060903", Quantity: 1}}))
	assert.Equal(t, []CartItem{{UPC: "0001111060903", Quantity: 1}}, added)
}

func TestCartAddReportsRejectionWithoutRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusServiceUnavailable, `{}`), nil
	})}
	cart := CartClient{HTTPClient: client}
	err := cart.Add(t.Context(), "shopper-token", []CartItem{{UPC: "123", Quantity: 1}})
	require.ErrorContains(t, err, "503")
	assert.Equal(t, 1, calls)
}

func TestCartAddResponses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusBadRequest, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
				var payload map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
				assert.Equal(t, map[string]any{"items": []any{map[string]any{"upc": "123", "quantity": float64(2)}}}, payload)
				return cartResponse(status, ""), nil
			})}
			cart := &CartClient{HTTPClient: client}
			err := cart.Add(t.Context(), "shopper", []CartItem{{UPC: "123", Quantity: 2}})
			if status < 300 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "kroger cart returned")
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCartAddEmptyDoesNotSend(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("empty cart must not be sent"); return nil, nil })}
	cart := &CartClient{HTTPClient: client}
	require.ErrorContains(t, cart.Add(t.Context(), "shopper", nil), "cart has no items")
}
