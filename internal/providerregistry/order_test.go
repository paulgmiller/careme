package providerregistry

import (
	"encoding/base64"
	"fmt"
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
	for _, path := range []string{"/kroger/order", "/kroger/callback"} {
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
	form := url.Values{"list_key": {"list"}, "location": {`{"id":"70500874","name":"QFC","chain":"QFC"}`}, "return_url": {"/recipes?h=list"}, "items": {`[{"id":"milk","name":"Milk"}]`}}
	request := httptest.NewRequest(http.MethodPost, "/kroger/order", strings.NewReader(form.Encode()))
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
			view, err := factory.OrderViews(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: scenario.location, Chain: "QFC"}})
			require.NoError(t, err)
			if scenario.want {
				require.Len(t, view, 1)
				assert.Equal(t, "/kroger/order", view[0].ActionURL)
				assert.Equal(t, "Add to QFC cart", view[0].ActionLabel)
			} else {
				assert.Nil(t, view)
			}
		})
	}
}

func TestInstacartOrderRegistration(t *testing.T) {
	for _, tt := range []struct {
		name, key, base  string
		enabled, invalid bool
	}{
		{name: "disabled"},
		{name: "enabled", key: "test-key", enabled: true},
		{name: "development", key: "test-key", base: "https://connect.dev.instacart.tools", enabled: true},
		{name: "invalid endpoint", key: "test-key", base: "https://evil.test", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := http.NewServeMux()
			factory := NewFactory(&config.Config{Instacart: config.InstacartConfig{APIKey: tt.key, BaseURL: tt.base}})
			err := factory.RegisterOrders(routes, orderingTestOptions())
			if tt.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest("POST", "/instacart/order", nil))
			if tt.enabled {
				assert.Equal(t, 400, w.Code)
			} else {
				assert.Equal(t, 404, w.Code)
			}
		})
	}
}

func TestOrderViewsIncludeBothDestinations(t *testing.T) {
	for _, tt := range []struct {
		name, location, instacart, kroger string
		actions                           []string
	}{
		{name: "both disabled", location: "any-store"},
		{name: "arbitrary store", location: "unknown-store", instacart: "key", actions: []string{"/instacart/order"}},
		{name: "Kroger both", location: "70500874", instacart: "key", kroger: "key", actions: []string{"/kroger/order", "/instacart/order"}},
		{name: "Kroger only Instacart", location: "70500874", instacart: "key", actions: []string{"/instacart/order"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewFactory(&config.Config{Instacart: config.InstacartConfig{APIKey: tt.instacart}, Kroger: config.KrogerConfig{CartTokenKey: tt.kroger}})
			views, err := factory.OrderViews(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: tt.location}, Query: url.Values{"kroger_status": {"denied"}, "instacart_status": {"failed"}}})
			require.NoError(t, err)
			require.Len(t, views, len(tt.actions))
			for i, action := range tt.actions {
				assert.Equal(t, action, views[i].ActionURL)
			}
			if len(views) == 2 {
				assert.Contains(t, views[0].Notice, "Kroger connection was not approved")
				assert.Contains(t, views[1].Notice, "Could not prepare your Instacart list")
			}
		})
	}
}

func TestBigCommerceOrderRegistration(t *testing.T) {
	for _, tt := range []struct {
		name             string
		cfg              config.MNFoodClubConfig
		enabled, invalid bool
	}{
		{name: "disabled"},
		{name: "enabled", cfg: config.MNFoodClubConfig{CartEnable: true, APIPath: "https://api.bigcommerce.com/stores/test/v3/", AccessToken: "test-token", CartOrigin: "https://groceries.example.test"}, enabled: true},
		{name: "missing credentials", cfg: config.MNFoodClubConfig{CartEnable: true}, invalid: true},
		{name: "missing cart origin", cfg: config.MNFoodClubConfig{CartEnable: true, APIPath: "https://api.bigcommerce.com/stores/test/v3/", AccessToken: "test-token"}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := http.NewServeMux()
			appRoutes := routing.Wrap(routes, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Order-Middleware", "applied")
					next.ServeHTTP(w, r)
				})
			})
			factory := NewFactory(&config.Config{MNFoodClub: tt.cfg})
			err := factory.RegisterOrders(appRoutes, orderingTestOptions())
			if tt.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			for _, path := range []string{"/bigcommerce/order", "/bigcommerce/cart"} {
				method := "GET"
				if path == "/bigcommerce/order" {
					method = "POST"
				}
				w := httptest.NewRecorder()
				routes.ServeHTTP(w, httptest.NewRequest(method, path, nil))
				if tt.enabled {
					assert.Equal(t, 400, w.Code)
					assert.Equal(t, "applied", w.Header().Get("X-Order-Middleware"))
				} else {
					assert.Equal(t, 404, w.Code)
				}
			}
		})
	}
}

func TestBigCommerceOrderViewsOnlyMatchConfiguredCatalog(t *testing.T) {
	for _, tt := range []struct {
		location string
		enabled  bool
		actions  []string
	}{
		{location: "mnfoodclub_delivery", enabled: true, actions: []string{"/bigcommerce/order", "/instacart/order"}},
		{location: "mnfoodclub_pickup", enabled: true, actions: []string{"/bigcommerce/order", "/instacart/order"}},
		{location: "70500874", enabled: true, actions: []string{"/instacart/order"}},
		{location: "unknown", enabled: true, actions: []string{"/instacart/order"}},
		{location: "mnfoodclub_delivery", enabled: false, actions: []string{"/instacart/order"}},
	} {
		t.Run(tt.location+fmt.Sprint(tt.enabled), func(t *testing.T) {
			factory := NewFactory(&config.Config{Instacart: config.InstacartConfig{APIKey: "configured"}, MNFoodClub: config.MNFoodClubConfig{CartEnable: tt.enabled}})
			views, err := factory.OrderViews(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: tt.location}})
			require.NoError(t, err)
			require.Len(t, views, len(tt.actions))
			for i, action := range tt.actions {
				assert.Equal(t, action, views[i].ActionURL)
			}
		})
	}
}
