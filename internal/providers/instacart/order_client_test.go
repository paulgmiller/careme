package instacart

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCreatePreferredRetailerLink(t *testing.T) {
	for _, tt := range []struct{ name, location, link, want string }{
		{"plain link", "safeway_1444", "https://www.instacart.com/store/shopping_lists/123", "safeway"},
		{"affiliate link", "aldi_F219", "https://www.instacart.com/store/shopping_lists/123?aff_id=7&offer_id=1#items", "aldi"},
		{"replace retailer", "publix_1083", "https://www.instacart.com/store/shopping_lists/123?retailer_key=other", "publix"},
		{"H-E-B", "heb_22", "https://www.instacart.com/store/shopping_lists/123", "heb"},
		{"no chain prefix", "any-store", "https://www.instacart.com/store/shopping_lists/123?aff_id=7", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewOrderClient(config.InstacartConfig{APIKey: "test-key"}, "https://careme.test", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.NotContains(t, payload, "retailer_key")
				body, err := json.Marshal(map[string]string{"products_link_url": tt.link})
				require.NoError(t, err)
				return response(http.StatusOK, string(body)), nil
			})})
			require.NoError(t, err)
			input := orderInput()
			input.Location.ID = tt.location
			link, err := client.create(t.Context(), input)
			require.NoError(t, err)
			got, err := url.Parse(link)
			require.NoError(t, err)
			original, err := url.Parse(tt.link)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Query().Get("retailer_key"))
			assert.Equal(t, original.Query().Get("aff_id"), got.Query().Get("aff_id"))
			assert.Equal(t, original.Query().Get("offer_id"), got.Query().Get("offer_id"))
			assert.Equal(t, original.Fragment, got.Fragment)
		})
	}
}

func TestCreateLink(t *testing.T) {
	for _, tt := range []struct {
		name, body   string
		status       int
		transportErr error
		want         string
	}{
		{name: "success", status: 200, body: `{"products_link_url":"https://www.instacart.com/store/shopping_lists/123?aff_id=7"}`},
		{name: "unauthorized", status: 401, body: `{"error":"secret details"}`, want: "returned 401"},
		{name: "rate limited", status: 429, want: "returned 429"},
		{name: "server failure", status: 503, want: "returned 503"},
		{name: "redirect", status: 302, want: "returned 302"},
		{name: "malformed", status: 200, body: `{`, want: "decode Instacart"},
		{name: "trailing data", status: 200, body: `{"products_link_url":"https://www.instacart.com/x"}{}`, want: "decode Instacart"},
		{name: "missing URL", status: 200, body: `{}`, want: "invalid shopping link"},
		{name: "unsafe URL", status: 200, body: `{"products_link_url":"https://instacart.com.evil.test/x"}`, want: "invalid shopping link"},
		{name: "oversized", status: 200, body: strings.Repeat("x", (1<<20)+1), want: "too large"},
		{name: "network", transportErr: errors.New("network down"), want: "create Instacart link"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client, err := NewOrderClient(config.InstacartConfig{APIKey: "test-key"}, "https://careme.test", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, productionURL+"/idp/v1/products/products_link", r.URL.String())
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.JSONEq(t, `{"title":"list"}`, string(body))
				if tt.transportErr != nil {
					return nil, tt.transportErr
				}
				return response(tt.status, tt.body), nil
			})})
			require.NoError(t, err)
			got, err := client.createLink(t.Context(), []byte(`{"title":"list"}`))
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				assert.Empty(t, got)
			} else {
				require.NoError(t, err)
				assert.Contains(t, got, "?aff_id=7")
			}
			assert.Equal(t, 1, calls, "POST must not be retried")
		})
	}
}

func TestNewOrderClientConfiguration(t *testing.T) {
	for _, tt := range []struct {
		key, base string
		valid     bool
	}{
		{"key", "", true},
		{"key", productionURL, true},
		{"key", developmentURL + "/", true},
		{"", productionURL, false},
		{"  ", productionURL, false},
		{"key", "https://evil.test", false},
	} {
		_, err := NewOrderClient(config.InstacartConfig{APIKey: tt.key, BaseURL: tt.base}, "https://careme.test", http.DefaultClient)
		if tt.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestValidateLink(t *testing.T) {
	for _, raw := range []string{"", "//www.instacart.com/x", "http://www.instacart.com/x", "javascript:alert(1)", "https://instacart.com.evil.test/x", "https://evilinstacart.com/x", "https://user:pass@instacart.com/x", "https://instacart.com:443/x", "https://instacart.com/\\evil", "https://instacart.com/\n"} {
		assert.Error(t, validateLink(raw, productionURL), raw)
		assert.Error(t, validateLink(raw, developmentURL), raw)
	}
	assert.NoError(t, validateLink("https://www.instacart.com/store/shopping_lists/1", productionURL))
}

func TestCreateLinkDevelopmentHost(t *testing.T) {
	for _, tt := range []struct {
		name, origin, link string
		valid              bool
	}{
		{"development", "http://localhost:8080", "https://customers.dev.instacart.tools/store/shopping_lists/1", true},
		{"production rejects development", "https://careme.test", "https://customers.dev.instacart.tools/store/shopping_lists/1", false},
		{"lookalike suffix", "http://localhost:8080", "https://customers.dev.instacart.tools.evil.test/list", false},
		{"lookalike prefix", "http://localhost:8080", "https://evilcustomers.dev.instacart.tools/list", false},
		{"other development subdomain", "http://localhost:8080", "https://other.dev.instacart.tools/list", false},
		{"HTTP", "http://localhost:8080", "http://customers.dev.instacart.tools/list", false},
		{"credentials", "http://localhost:8080", "https://user:pass@customers.dev.instacart.tools/list", false},
		{"port", "http://localhost:8080", "https://customers.dev.instacart.tools:443/list", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"products_link_url": tt.link})
			require.NoError(t, err)
			client, err := NewOrderClient(config.InstacartConfig{APIKey: "test-key"}, tt.origin, &http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) {
				return response(http.StatusOK, string(body)), nil
			})})
			require.NoError(t, err)
			link, err := client.createLink(t.Context(), []byte(`{}`))
			if tt.valid {
				require.NoError(t, err)
				assert.Equal(t, tt.link, link)
			} else {
				require.ErrorContains(t, err, "invalid shopping link")
				assert.Empty(t, link)
			}
		})
	}
}

func TestOrderClientDoesNotFollowRedirects(t *testing.T) {
	calls := 0
	client, err := NewOrderClient(config.InstacartConfig{APIKey: "key"}, "https://careme.test", &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			assert.Equal(t, "connect.instacart.com", r.URL.Host)
			resp := response(302, "")
			resp.Header.Set("Location", "https://evil.test/capture")
			return resp, nil
		}),
	})
	require.NoError(t, err)
	_, err = client.createLink(t.Context(), []byte(`{}`))
	require.ErrorContains(t, err, "returned 302")
	assert.Equal(t, 1, calls)
}

func TestOrderClientRequiresPublicOrigin(t *testing.T) {
	for _, origin := range []string{"", "https://careme.test/path", "//careme.test", "https://user:pass@careme.test", "https://careme.test?query=bad"} {
		_, err := NewOrderClient(config.InstacartConfig{APIKey: "key"}, origin, http.DefaultClient)
		require.ErrorContains(t, err, "public origin")
	}
}

func TestOrderClientSelectsEndpointForOrigin(t *testing.T) {
	for _, tt := range []struct{ name, origin, base, want string }{
		{name: "localhost", origin: "http://localhost:8080", want: developmentURL},
		{name: "HTTPS localhost", origin: "https://LOCALHOST:8443", want: developmentURL},
		{name: "IPv4 loopback", origin: "http://127.0.0.1:8080", want: developmentURL},
		{name: "other IPv4 loopback", origin: "http://127.0.0.2:8080", want: developmentURL},
		{name: "IPv6 loopback", origin: "http://[::1]:8080", want: developmentURL},
		{name: "localhost overrides production", origin: "http://localhost:8080", base: productionURL, want: developmentURL},
		{name: "loopback overrides production", origin: "http://127.0.0.1:8080", base: productionURL, want: developmentURL},
		{name: "nonlocal default", origin: "https://careme.test", want: productionURL},
		{name: "localhost is a subdomain", origin: "https://localhost.example.test", want: productionURL},
		{name: "explicit production", origin: "https://careme.test", base: productionURL, want: productionURL},
		{name: "explicit development", origin: "https://careme.test", base: developmentURL, want: developmentURL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client, err := NewOrderClient(config.InstacartConfig{APIKey: "test-key", BaseURL: tt.base}, tt.origin, &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, tt.want+"/idp/v1/products/products_link", r.URL.String())
				return response(http.StatusOK, `{"products_link_url":"https://www.instacart.com/list"}`), nil
			})})
			require.NoError(t, err)
			_, err = client.createLink(t.Context(), []byte(`{"title":"test"}`))
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestCreateLinkRejectionDiagnostics(t *testing.T) {
	for _, tt := range []struct{ name, origin, link, want, api string }{
		{name: "missing field", origin: "https://careme.test", want: "products_link_url is missing or empty", api: productionURL},
		{name: "development response host", origin: "http://localhost:8080", link: "https://dev.example.test/store/list?token=secret-query", want: `unapproved host "dev.example.test"`, api: developmentURL},
		{name: "HTTP response", origin: "https://careme.test", link: "http://www.instacart.com/list?token=secret-query", want: "must use HTTPS", api: productionURL},
		{name: "credentials", origin: "https://careme.test", link: "https://chef:secret-password@www.instacart.com/list?token=secret-query", want: "contains credentials", api: productionURL},
		{name: "explicit port", origin: "https://careme.test", link: "https://www.instacart.com:443/list?token=secret-query", want: `explicit port "443"`, api: productionURL},
		{name: "malformed URL", origin: "https://careme.test", link: "https://www.instacart.com/%ZZ?token=secret-query", want: "is malformed", api: productionURL},
		{name: "API key reflected in host", origin: "https://careme.test", link: "https://private-api-key.example.test/list?token=secret-query", want: `unapproved host "[redacted].example.test"`, api: productionURL},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"products_link_url": tt.link})
			require.NoError(t, err)
			client, err := NewOrderClient(config.InstacartConfig{APIKey: "private-api-key"}, tt.origin, &http.Client{Transport: transportFunc(func(_ *http.Request) (*http.Response, error) { return response(http.StatusOK, string(body)), nil })})
			require.NoError(t, err)
			_, err = client.createLink(t.Context(), []byte(`{}`))
			require.ErrorContains(t, err, tt.want)
			assert.Contains(t, err.Error(), "API "+tt.api)
			for _, secret := range []string{"secret-query", "secret-password", "private-api-key", "chef", "?token"} {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
}
