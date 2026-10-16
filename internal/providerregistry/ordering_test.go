package providerregistry

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ordering"
	"careme/internal/providers/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderingTestOptions() OrderingOptions {
	return OrderingOptions{
		Cache: cache.NewInMemoryCache(), Auth: auth.DefaultMock(),
		Resume: func(http.ResponseWriter, *http.Request, string, string, string) {},
	}
}

func TestNewOrderingDisabled(t *testing.T) {
	orders, err := NewFactory(&config.Config{}).NewOrdering(orderingTestOptions())
	require.NoError(t, err)
	assert.Empty(t, orders.Service.Providers())
	routes := http.NewServeMux()
	orders.Register(routes)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kroger/callback", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewOrderingInvalidKrogerConfig(t *testing.T) {
	validKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	for _, tt := range []struct {
		name string
		cfg  config.KrogerConfig
		want string
	}{
		{"missing client ID", config.KrogerConfig{ClientSecret: "secret", CartTokenKey: validKey}, "requires client ID and secret"},
		{"missing secret", config.KrogerConfig{ClientID: "client", CartTokenKey: validKey}, "requires client ID and secret"},
		{"invalid base64", config.KrogerConfig{ClientID: "client", ClientSecret: "secret", CartTokenKey: "!"}, "base64-encoded 32-byte key"},
		{"wrong key length", config.KrogerConfig{ClientID: "client", ClientSecret: "secret", CartTokenKey: base64.StdEncoding.EncodeToString([]byte("short"))}, "base64-encoded 32-byte key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			orders, err := NewFactory(&config.Config{Kroger: tt.cfg}).NewOrdering(orderingTestOptions())
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, orders)
		})
	}
}

type orderingRoundTrip func(*http.Request) (*http.Response, error)

func (f orderingRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewOrderingConnectsAndResumesKroger(t *testing.T) {
	transport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = transport })
	var exchanges int
	http.DefaultTransport = orderingRoundTrip(func(r *http.Request) (*http.Response, error) {
		exchanges++
		assert.Equal(t, "https://api.kroger.com/v1/connect/oauth2/token", r.URL.String())
		assert.Equal(t, http.MethodPost, r.Method)
		clientID, secret, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "client", clientID)
		assert.Equal(t, "secret", secret)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		assert.Equal(t, "code", r.Form.Get("code"))
		assert.Equal(t, "https://careme.test/kroger/callback", r.Form.Get("redirect_uri"))
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"shopper","refresh_token":"refresh","expires_in":3600}`)), Request: r}, nil
	})

	options := orderingTestOptions()
	var resumed []string
	options.Resume = func(w http.ResponseWriter, r *http.Request, providerID, userID, hash string) {
		resumed = []string{providerID, userID, hash}
		w.WriteHeader(http.StatusNoContent)
	}
	orders, err := NewFactory(&config.Config{
		PublicOrigin: "https://careme.test/",
		Kroger:       config.KrogerConfig{ClientID: "client", ClientSecret: "secret", CartTokenKey: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))},
	}).NewOrdering(options)
	require.NoError(t, err)
	require.Len(t, orders.Service.Providers(), 1)
	provider, ok := orders.Service.Provider("kroger")
	require.True(t, ok)
	adapter := provider.(*kroger.OrderingAdapter)
	start := httptest.NewRecorder()
	adapter.StartAuthorization(start, httptest.NewRequest(http.MethodPost, "/recipes/hash/ordering/kroger", nil), ordering.Request{
		UserID: "mock-clerk-user-id", Hash: "hash", Source: ordering.Store{Chain: "QFC"},
	})
	require.Equal(t, http.StatusSeeOther, start.Code)
	authorization, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "client", authorization.Query().Get("client_id"))
	assert.Equal(t, "https://careme.test/kroger/callback", authorization.Query().Get("redirect_uri"))
	assert.Equal(t, "qfc", authorization.Query().Get("banner"))
	require.NotEmpty(t, authorization.Query().Get("state"))
	cookies := start.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure)

	routes := http.NewServeMux()
	orders.Register(routes)
	callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?code=code&state="+url.QueryEscape(authorization.Query().Get("state")), nil)
	callback.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, callback)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, exchanges)
	assert.Equal(t, []string{"kroger", "mock-clerk-user-id", "hash"}, resumed)
	token, err := adapter.LoadToken(t.Context(), "mock-clerk-user-id")
	require.NoError(t, err)
	assert.Equal(t, "shopper", token.AccessToken)
	assert.Equal(t, "refresh", token.RefreshToken)
}
