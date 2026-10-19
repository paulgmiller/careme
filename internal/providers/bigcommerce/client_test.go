package bigcommerce

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCartID = "f032759b-20de-4c48-8af2-eaf37debd679"
	testAPI    = "https://api.bigcommerce.com/stores/test/v3/"
	testOrigin = "https://groceries.example.test"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func testClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewClient(ClientOptions{APIPath: testAPI, AccessToken: "test-token", CartOrigin: testOrigin, Transport: transport})
	require.NoError(t, err)
	return client
}

const availableProduct = `{"data":{"id":123,"is_visible":true,"availability":"available","inventory_tracking":"variant","variants":[{"id":8,"calculated_price":1,"purchasing_disabled":true},{"id":9,"calculated_price":2,"inventory_level":0},{"id":10,"calculated_price":5,"inventory_level":3},{"id":11,"calculated_price":4,"inventory_level":2}]}}`

func TestResolveProduct(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		want       error
		failure    string
		variant    int
	}{
		{name: "cheapest purchasable variant", body: availableProduct, status: 200, variant: 11},
		{name: "untracked zero stock", body: `{"data":{"id":123,"is_visible":true,"availability":"available","variants":[{"id":10,"calculated_price":0,"inventory_level":0}]}}`, status: 200, variant: 10},
		{name: "deleted", status: 404, want: ErrUnavailable},
		{name: "invisible", body: `{"data":{"id":123,"availability":"available"}}`, status: 200, want: ErrUnavailable},
		{name: "unavailable", body: `{"data":{"id":123,"is_visible":true,"availability":"disabled"}}`, status: 200, want: ErrUnavailable},
		{name: "product stock", body: `{"data":{"id":123,"is_visible":true,"availability":"available","inventory_tracking":"product","inventory_level":0}}`, status: 200, want: ErrUnavailable},
		{name: "required modifiers", body: `{"data":{"id":123,"is_visible":true,"availability":"available","modifiers":[{"required":true}]}}`, status: 200, want: ErrUnavailable},
		{name: "all variants disabled", body: `{"data":{"id":123,"is_visible":true,"availability":"available","variants":[{"id":10,"purchasing_disabled":true}]}}`, status: 200, want: ErrUnavailable},
		{name: "wrong ID", body: `{"data":{"id":99}}`, status: 200, failure: "ID does not match"},
		{name: "missing variants", body: `{"data":{"id":123,"is_visible":true,"availability":"available"}}`, status: 200, failure: "missing included variants"},
		{name: "invalid variant", body: `{"data":{"id":123,"is_visible":true,"availability":"available","variants":[{"id":0,"calculated_price":4}]}}`, status: 200, failure: "invalid variant data"},
		{name: "malformed", body: `{`, status: 200, failure: "decode BigCommerce"},
		{name: "rate limit", status: 429, failure: "HTTP 429"},
		{name: "unauthorized", status: 401, failure: "HTTP 401"},
		{name: "server error", status: 503, failure: "HTTP 503"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, testAPI+"catalog/products/123?include=variants,modifiers", r.URL.String())
				assert.Equal(t, "test-token", r.Header.Get("X-Auth-Token"))
				return response(tt.status, tt.body), nil
			}))
			line, err := client.Resolve(t.Context(), 123)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			} else if tt.failure != "" {
				require.ErrorContains(t, err, tt.failure)
			} else {
				require.NoError(t, err)
				assert.Equal(t, LineItem{ProductID: 123, VariantID: tt.variant, Quantity: 1}, line)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCreateCartPayload(t *testing.T) {
	client := testClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, testAPI+"carts", r.URL.String())
		assert.Equal(t, "test-token", r.Header.Get("X-Auth-Token"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"customer_id":0,"line_items":[{"product_id":123,"variant_id":11,"quantity":2}]}`, string(body))
		return response(201, `{"data":{"id":"`+testCartID+`"}}`), nil
	}))
	id, err := client.CreateCart(t.Context(), []LineItem{{ProductID: 123, VariantID: 11, Quantity: 2}})
	require.NoError(t, err)
	assert.Equal(t, testCartID, id)
}

func TestCreateCartFailuresAreNotReplayed(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		err        error
		want       string
	}{
		{name: "network", err: errors.New("network down"), want: "request BigCommerce"},
		{name: "rate limit", status: 429, want: "HTTP 429"},
		{name: "server failure", status: 500, want: "HTTP 500"},
		{name: "redirect", status: 302, want: "HTTP 302"},
		{name: "invalid ID", status: 201, body: `{"data":{"id":"../bad"}}`, want: "invalid cart ID"},
		{name: "malformed", status: 201, body: `{`, want: "decode BigCommerce"},
		{name: "oversized", status: 201, body: strings.Repeat("x", (2<<20)+1), want: "too large"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, transportFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				if tt.err != nil {
					return nil, tt.err
				}
				return response(tt.status, tt.body), nil
			}))
			_, err := client.CreateCart(t.Context(), []LineItem{{ProductID: 123, Quantity: 1}})
			require.ErrorContains(t, err, tt.want)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestFreshCartRedirects(t *testing.T) {
	calls := 0
	client := testClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, testAPI+"carts/"+testCartID+"/redirect_urls", r.URL.String())
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(body))
		return response(201, `{"data":{"cart_url":"`+testOrigin+`/cart.php?action=load&token=fresh"}}`), nil
	}))
	for range 2 {
		destination, err := client.CartRedirect(t.Context(), testCartID)
		require.NoError(t, err)
		assert.Contains(t, destination, "token=fresh")
	}
	assert.Equal(t, 2, calls)
	for _, destination := range []string{"", "https://evil.test/cart.php", "http://groceries.example.test/cart.php", testOrigin + "/other", "https://user:pass@groceries.example.test/cart.php", "//groceries.example.test/cart.php"} {
		client := testClient(t, transportFunc(func(_ *http.Request) (*http.Response, error) {
			return response(201, `{"data":{"cart_url":"`+destination+`"}}`), nil
		}))
		_, err := client.CartRedirect(t.Context(), testCartID)
		require.ErrorContains(t, err, "invalid cart redirect")
	}
	_, err := client.CartRedirect(t.Context(), "../bad")
	require.ErrorContains(t, err, "invalid BigCommerce cart ID")
}

func TestClientConfiguration(t *testing.T) {
	for _, tt := range []struct{ api, token, origin string }{
		{"", "token", testOrigin},
		{"https://evil.test/stores/test/v3/", "token", testOrigin},
		{"http://api.bigcommerce.com/stores/test/v3/", "token", testOrigin},
		{testAPI + "?token=bad", "token", testOrigin},
		{testAPI, "", testOrigin},
		{testAPI, "token", ""},
		{testAPI, "token", "http://groceries.example.test"},
		{testAPI, "token", testOrigin + "/checkout"},
	} {
		_, err := NewClient(ClientOptions{APIPath: tt.api, AccessToken: tt.token, CartOrigin: tt.origin})
		require.Error(t, err)
	}
	client := testClient(t, transportFunc(func(_ *http.Request) (*http.Response, error) { return nil, errors.New("unused") }))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := client.Resolve(ctx, 123)
	require.Error(t, err)
}
