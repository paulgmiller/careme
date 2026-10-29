package providerregistry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstacartOrderRegistration(t *testing.T) {
	for _, tt := range []struct {
		name, key, base         string
		enabled, invalid, mocks bool
	}{
		{name: "disabled"},
		{name: "enabled", key: "key", enabled: true},
		{name: "development", key: "key", base: "https://connect.dev.instacart.tools", enabled: true},
		{name: "invalid endpoint", key: "key", base: "https://evil.test", invalid: true},
		{name: "mock precedence", key: "key", base: "invalid", mocks: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := http.NewServeMux()
			appRoutes := routing.Wrap(routes, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Order-Middleware", "applied")
					next.ServeHTTP(w, r)
				})
			})
			factory := NewFactory(&config.Config{Instacart: config.InstacartConfig{APIKey: tt.key, BaseURL: tt.base}, Mocks: config.MockConfig{Enable: tt.mocks}})
			err := factory.RegisterOrders(appRoutes, orderingTestOptions())
			if tt.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest("POST", "/instacart/order-transfers", nil))
			if tt.enabled {
				assert.Equal(t, 400, w.Code)
				assert.Equal(t, "applied", w.Header().Get("X-Order-Middleware"))
			} else {
				assert.Equal(t, 404, w.Code)
			}
			w = httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest("POST", "/instacart/order", nil))
			assert.Equal(t, 404, w.Code)
		})
	}
}

func TestInstacartOrderViewUsesNativeProviderPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name, location, want string
		cfg                  config.Config
	}{
		{name: "disabled", location: "any-store"},
		{name: "any store", location: "unknown-store", cfg: config.Config{Instacart: config.InstacartConfig{APIKey: "key"}}, want: "/instacart/order-transfers"},
		{name: "Kroger unconfigured", location: "70500874", cfg: config.Config{Instacart: config.InstacartConfig{APIKey: "key"}}, want: "/instacart/order-transfers"},
		{name: "Kroger preferred", location: "70500874", cfg: config.Config{Kroger: config.KrogerConfig{CartTokenKey: "configured"}, Instacart: config.InstacartConfig{APIKey: "key"}}, want: "/kroger/order-transfers"},
		{name: "MNFoodClub preferred", location: "mnfoodclub_delivery", cfg: config.Config{MNFoodClub: config.MNFoodClubConfig{OrderAPIURL: "https://foodclub.test", OrderAccessToken: "key"}, Instacart: config.InstacartConfig{APIKey: "key"}}, want: "/mnfoodclub/order-transfers"},
		{name: "mock preferred", location: "any-store", cfg: config.Config{Mocks: config.MockConfig{Enable: true}, Instacart: config.InstacartConfig{APIKey: "key"}}, want: "/mock/order-transfers"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			options := orderingTestOptions()
			builder := NewFactory(&tt.cfg).NewOrderViewBuilder(options.Cache)
			input := ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list", Location: locationtypes.Location{ID: tt.location}, ReturnURL: "/recipes?h=list"}
			view, err := builder.OrderView(t.Context(), input)
			require.NoError(t, err)
			if tt.want == "" {
				assert.Nil(t, view)
				return
			}
			require.NotNil(t, view)
			assert.Equal(t, tt.want, view.TransferURL)
			if tt.want == "/instacart/order-transfers" {
				require.NoError(t, options.Cache.Put(t.Context(), "instacart_order_transfers/viewer/list", `{"transfer_id":"list","status":"applied","cart_url":"https://www.instacart.com/list"}`, cache.Unconditional()))
				view, err = builder.OrderView(t.Context(), input)
				require.NoError(t, err)
				assert.Equal(t, "https://www.instacart.com/list", view.ReviewCartURL)
				assert.Empty(t, view.TransferURL)
			}
		})
	}
}
