package providerregistry

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderingTestOptions() KrogerOrderOptions {
	return KrogerOrderOptions{
		Cache: cache.NewInMemoryCache(), Auth: auth.DefaultMock(),
	}
}

func TestNewKrogerOrderDisabled(t *testing.T) {
	orders, err := NewFactory(&config.Config{}).NewKrogerOrder(orderingTestOptions())
	require.NoError(t, err)
	assert.Nil(t, orders)
}

func TestNewKrogerOrderInvalidKrogerConfig(t *testing.T) {
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
			orders, err := NewFactory(&config.Config{Kroger: tt.cfg}).NewKrogerOrder(orderingTestOptions())
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, orders)
		})
	}
}

func TestNewKrogerOrderRegistersHTTPRoutes(t *testing.T) {
	handler, err := NewFactory(&config.Config{PublicOrigin: "https://careme.test", Kroger: config.KrogerConfig{ClientID: "client", ClientSecret: "secret", CartTokenKey: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}}).NewKrogerOrder(orderingTestOptions())
	require.NoError(t, err)
	routes := http.NewServeMux()
	handler.Register(routes)
	form := url.Values{"list_key": {"list"}, "store_id": {"70500874"}, "chain": {"QFC"}, "return_url": {"/recipes?h=list"}, "items": {`[{"id":"milk","name":"Milk"}]`}}
	request := httptest.NewRequest(http.MethodPost, "/kroger/order", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	destination, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "client", destination.Query().Get("client_id"))
	assert.Equal(t, "https://careme.test/kroger/callback", destination.Query().Get("redirect_uri"))
	assert.Equal(t, "qfc", destination.Query().Get("banner"))
	callback := httptest.NewRecorder()
	routes.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/kroger/callback", nil))
	assert.Equal(t, http.StatusBadRequest, callback.Code)
}
