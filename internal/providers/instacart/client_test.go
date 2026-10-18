package instacart

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
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
			client, err := NewClient(ClientOptions{APIKey: "test-key", Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, ProductionURL+"/idp/v1/products/products_link", r.URL.String())
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

func TestNewClientConfiguration(t *testing.T) {
	for _, tt := range []struct {
		key, base string
		valid     bool
	}{
		{"key", "", true},
		{"key", ProductionURL, true},
		{"key", DevelopmentURL + "/", true},
		{"", ProductionURL, false},
		{"  ", ProductionURL, false},
		{"key", "https://evil.test", false},
	} {
		_, err := NewClient(ClientOptions{APIKey: tt.key, BaseURL: tt.base})
		if tt.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestValidLink(t *testing.T) {
	for _, raw := range []string{"", "//www.instacart.com/x", "http://www.instacart.com/x", "javascript:alert(1)", "https://instacart.com.evil.test/x", "https://evilinstacart.com/x", "https://user:pass@instacart.com/x", "https://instacart.com:443/x", "https://instacart.com/\\evil", "https://instacart.com/\n"} {
		assert.False(t, validLink(raw), raw)
	}
	assert.True(t, validLink("https://www.instacart.com/store/shopping_lists/1"))
}
