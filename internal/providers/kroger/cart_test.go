package kroger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cartRoundTrip func(*http.Request) (*http.Response, error)

func (f cartRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func cartResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}

func TestCartClientConnectLookupAndAdd(t *testing.T) {
	var added []cartItem
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/connect/oauth2/token":
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "grant_type=authorization_code")
			return cartResponse(http.StatusOK, `{"access_token":"shopper-token","refresh_token":"refresh","expires_in":3600}`), nil
		case "/v1/products/0001111060903":
			assert.Equal(t, "01400943", req.URL.Query().Get("filter.locationId"))
			assert.Equal(t, "Bearer catalog-token", req.Header.Get("Authorization"))
			return cartResponse(http.StatusOK, `{"data":{"items":[{"itemId":"","inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}},{"itemId":"0001111060903","inventory":{"stockLevel":"HIGH"}}]}}`), nil
		case "/v1/cart/add":
			assert.Equal(t, http.MethodPut, req.Method)
			assert.Equal(t, "Bearer shopper-token", req.Header.Get("Authorization"))
			var body struct {
				Items []cartItem `json:"items"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			added = body.Items
			return cartResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected Kroger request: %s", req.URL)
			return nil, nil
		}
	})}
	manager := NewKrogerTokenManager("id", "secret", client)
	manager.token = "catalog-token"
	manager.expiresAt = time.Now().Add(time.Hour)
	cart := CartClient{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://careme.test/kroger/callback", HTTPClient: client, CatalogToken: manager}
	destination, err := cart.authorizationURL("nonce", "qfc")
	require.NoError(t, err)
	parsed, err := url.Parse(destination)
	require.NoError(t, err)
	assert.Equal(t, "cart.basic:write", parsed.Query().Get("scope"))
	assert.Equal(t, "nonce", parsed.Query().Get("state"))
	assert.Equal(t, "qfc", parsed.Query().Get("banner"))
	token, err := cart.exchange(context.Background(), "code")
	require.NoError(t, err)
	upc, err := cart.productUPC(context.Background(), "0001111060903", "01400943")
	require.NoError(t, err)
	require.NoError(t, cart.add(context.Background(), token.AccessToken, []cartItem{{UPC: upc, Quantity: 1}}))
	assert.Equal(t, []cartItem{{UPC: "0001111060903", Quantity: 1}}, added)
}

func TestCartProductUnavailable(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		return cartResponse(http.StatusNotFound, `{}`), nil
	})}
	manager := NewKrogerTokenManager("id", "secret", client)
	manager.token = "catalog-token"
	manager.expiresAt = time.Now().Add(time.Hour)
	cart := CartClient{HTTPClient: client, CatalogToken: manager}
	_, err := cart.productUPC(context.Background(), "missing", "01400943")
	require.ErrorIs(t, err, errProductUnavailable)
}

func TestCartAddReportsRejectionWithoutRetry(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusServiceUnavailable, `{}`), nil
	})}
	cart := CartClient{HTTPClient: client}
	err := cart.add(t.Context(), "shopper-token", []cartItem{{UPC: "123", Quantity: 1}})
	require.ErrorContains(t, err, "503")
	assert.Equal(t, 1, calls)
}

func TestCartProductLookupDoesNotRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(status, `{}`), nil
			})}
			manager := NewKrogerTokenManager("id", "secret", client)
			manager.token, manager.expiresAt = "catalog", time.Now().Add(time.Hour)
			cart := CartClient{HTTPClient: client, CatalogToken: manager}
			_, err := cart.productUPC(t.Context(), "food", "store")
			require.Error(t, err)
			assert.Equal(t, 1, calls)
		})
	}
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
			err := cart.add(t.Context(), "shopper", []cartItem{{UPC: "123", Quantity: 2}})
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
	require.ErrorContains(t, cart.add(t.Context(), "shopper", nil), "cart has no items")
}

func TestCartProductResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		unavailable      bool
	}{
		{"malformed", `{`, "look up Kroger product", false},
		{"missing data", `{}`, "missing product data", false},
		{"no items", `{"data":{}}`, "no UPC", true},

		{"empty UPC", `{"data":{"items":[{"itemId":" "}]}}`, "no UPC", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) { return cartResponse(200, tc.body), nil })}
			manager := NewKrogerTokenManager("client", "secret", client)
			manager.token = "catalog"
			manager.expiresAt = time.Now().Add(time.Hour)
			cart := &CartClient{HTTPClient: client, CatalogToken: manager}
			_, err := cart.productUPC(t.Context(), "123", "store")
			require.ErrorContains(t, err, tc.want)
			if tc.unavailable {
				require.ErrorIs(t, err, errProductUnavailable)
			}
		})
	}
}
