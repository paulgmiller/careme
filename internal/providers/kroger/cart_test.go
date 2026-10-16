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
	var added []CartItem
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
			return cartResponse(http.StatusOK, `{"data":{"items":[{"itemId":"out","inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}},{"itemId":"0001111060903","inventory":{"stockLevel":"HIGH"}}]}}`), nil
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
	manager := NewKrogerTokenManager("id", "secret", client)
	manager.token = "catalog-token"
	manager.expiresAt = time.Now().Add(time.Hour)
	cart := CartClient{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://careme.test/kroger/callback", HTTPClient: client, CatalogToken: manager}
	parsed, err := url.Parse(cart.AuthorizationURL("nonce", "qfc"))
	require.NoError(t, err)
	assert.Equal(t, "cart.basic:write", parsed.Query().Get("scope"))
	assert.Equal(t, "nonce", parsed.Query().Get("state"))
	assert.Equal(t, "qfc", parsed.Query().Get("banner"))
	token, err := cart.Exchange(context.Background(), "code")
	require.NoError(t, err)
	upc, err := cart.ProductUPC(context.Background(), "0001111060903", "01400943")
	require.NoError(t, err)
	require.NoError(t, cart.Add(context.Background(), token.AccessToken, []CartItem{{UPC: upc, Quantity: 1}}))
	assert.Equal(t, []CartItem{{UPC: "0001111060903", Quantity: 1}}, added)
}

func TestCartProductUnavailable(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		return cartResponse(http.StatusNotFound, `{}`), nil
	})}
	manager := NewKrogerTokenManager("id", "secret", client)
	manager.token = "catalog-token"
	manager.expiresAt = time.Now().Add(time.Hour)
	cart := CartClient{HTTPClient: client, CatalogToken: manager}
	_, err := cart.ProductUPC(context.Background(), "missing", "01400943")
	require.ErrorIs(t, err, ErrProductUnavailable)
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
