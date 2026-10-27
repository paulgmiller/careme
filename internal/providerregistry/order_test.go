package providerregistry

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ordertypes "careme/internal/orders/types"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	"careme/internal/routing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderingTestOptions() OrderOptions {
	return OrderOptions{
		Cache: cache.NewInMemoryCache(), Auth: auth.DefaultMock(),
	}
}

func TestRegisterOrdersDisabled(t *testing.T) {
	routes := http.NewServeMux()
	err := NewFactory(&config.Config{}).RegisterOrders(routes, orderingTestOptions())
	require.NoError(t, err)
	for _, path := range []string{"/kroger/cart-transfers", "/kroger/callback"} {
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, response.Code)
	}
}

func TestRegisterOrdersInvalidKrogerConfig(t *testing.T) {
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
			routes := http.NewServeMux()
			err := NewFactory(&config.Config{Kroger: tt.cfg}).RegisterOrders(routes, orderingTestOptions())
			require.ErrorContains(t, err, tt.want)
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/kroger/callback", nil))
			assert.Equal(t, http.StatusNotFound, response.Code)
		})
	}
}

func TestRegisterOrdersHTTPRoutes(t *testing.T) {
	routes := http.NewServeMux()
	appRoutes := routing.Wrap(routes, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Order-Middleware", "applied")
			next.ServeHTTP(w, r)
		})
	})
	err := NewFactory(&config.Config{PublicOrigin: "https://careme.test", Kroger: config.KrogerConfig{ClientID: "client", ClientSecret: "secret", CartTokenKey: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))}}).RegisterOrders(appRoutes, orderingTestOptions())
	require.NoError(t, err)
	form := url.Values{"idempotency_key": {"list"}, "transfer_request": {`{"location":{"id":"70500874","name":"QFC","chain":"QFC"},"return_url":"/recipes?h=list","items":[{"id":"milk","name":"Milk"}]}`}}
	request := httptest.NewRequest(http.MethodPost, "/kroger/cart-transfers", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "applied", response.Header().Get("X-Order-Middleware"))
	destination, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "client", destination.Query().Get("client_id"))
	assert.Equal(t, "https://careme.test/kroger/callback", destination.Query().Get("redirect_uri"))
	assert.Equal(t, "qfc", destination.Query().Get("banner"))
	callback := httptest.NewRecorder()
	routes.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/kroger/callback", nil))
	assert.Equal(t, http.StatusBadRequest, callback.Code)
	assert.Equal(t, "applied", callback.Header().Get("X-Order-Middleware"))
}

func TestOrderViewMatchesConfiguredLocation(t *testing.T) {
	for _, scenario := range []struct {
		name, key, location string
		want                bool
	}{
		{"Kroger enabled", "configured", "70500874", true},
		{"Kroger disabled", "", "70500874", false},
		{"other provider", "configured", "walmart_123", false},
		{"unknown location", "configured", "unknown", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			factory := NewFactory(&config.Config{Kroger: config.KrogerConfig{CartTokenKey: scenario.key}})
			view, err := factory.OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: scenario.location, Chain: "QFC"}})
			require.NoError(t, err)
			if scenario.want {
				require.NotNil(t, view)
				assert.Equal(t, "/kroger/cart-transfers", view.TransferURL)
				assert.Equal(t, "Add to QFC cart", view.ActionLabel)
			} else {
				assert.Nil(t, view)
			}
		})
	}
}
