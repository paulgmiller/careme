package providerregistry

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	ordertypes "careme/internal/orders/types"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/providers/instacart"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/mockorder"
	"careme/internal/providers/wholefoods"
	"careme/internal/routing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type OrderOptions struct {
	Cache cache.Cache
	Auth  auth.AuthClient
}

// RegisterOrders installs routes for every configured ordering destination.
func (f Factory) RegisterOrders(routes routing.Registrar, options OrderOptions) error {
	if f.config.Mocks.Enable {
		mockorder.NewHandler(options.Cache, options.Auth).Register(routes)
		return nil
	}
	if f.config.Kroger.CartTokenKey != "" {
		handler, err := f.newKrogerOrder(options)
		if err != nil {
			return err
		}
		handler.Register(routes)
	}
	if f.config.MNFoodClub.OrderAPIURL != "" || f.config.MNFoodClub.OrderAccessToken != "" {
		client, err := mnfoodclub.NewOrderClient(f.config.MNFoodClub, newOrderHTTPClient())
		if err != nil {
			return err
		}
		mnfoodclub.NewOrderHandler(options.Cache, options.Auth, client).Register(routes)
	}
	if f.config.Instacart.APIKey != "" {
		client, err := instacart.NewOrderClient(f.config.Instacart, f.config.ResolvedPublicOrigin(), newOrderHTTPClient())
		if err != nil {
			return err
		}
		instacart.NewOrderHandler(options.Cache, options.Auth, client).Register(routes)
	}
	return nil
}

func (f Factory) newKrogerOrder(options OrderOptions) (*kroger.OrderHandler, error) {
	cfg := f.config
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return nil, fmt.Errorf("kroger cart requires client ID and secret")
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Kroger.CartTokenKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("KROGER_CART_TOKEN_KEY must be a base64-encoded 32-byte key")
	}
	httpClient := newOrderHTTPClient()
	client := &kroger.CartClient{
		ClientID: cfg.Kroger.ClientID, ClientSecret: cfg.Kroger.ClientSecret,
		RedirectURI: cfg.ResolvedPublicOrigin() + "/kroger/callback",
		HTTPClient:  httpClient,
	}
	handler, err := kroger.NewOrderHandler(kroger.OrderOptions{
		Cache: options.Cache, Client: client, TokenKey: key,
		Origin: cfg.ResolvedPublicOrigin(), Auth: options.Auth,
	})
	if err != nil {
		return nil, fmt.Errorf("create Kroger order handler: %w", err)
	}
	return handler, nil
}

// Ordering clients are instrumented once and do not automatically retry or follow API redirects.
func newOrderHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       15 * time.Second,
		Transport:     otelhttp.NewTransport(http.DefaultTransport),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// orderViewBuilder binds provider-owned transfer reads to the application's cache.
type orderViewBuilder struct {
	factory Factory
	cache   cache.Cache
}

func (f Factory) NewOrderViewBuilder(c cache.Cache) *orderViewBuilder {
	return &orderViewBuilder{factory: f, cache: c}
}

func (b *orderViewBuilder) OrderView(ctx context.Context, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if b.factory.config.Mocks.Enable {
		return mockorder.OrderView(ctx, b.cache, input)
	}
	if kroger.NewIdentityProvider().IsID(input.Location.ID) && b.factory.config.Kroger.CartTokenKey != "" {
		return kroger.OrderViewForUser(ctx, b.cache, input)
	}
	if mnfoodclub.NewIdentityProvider().IsID(input.Location.ID) && (b.factory.config.MNFoodClub.OrderAPIURL != "" || b.factory.config.MNFoodClub.OrderAccessToken != "") {
		client, err := mnfoodclub.NewOrderClient(b.factory.config.MNFoodClub, newOrderHTTPClient())
		if err != nil {
			return nil, err
		}
		return mnfoodclub.OrderViewForUser(ctx, b.cache, client, input)
	}
	if wholefoods.NewIdentityProvider().IsID(input.Location.ID) && b.factory.config.Instacart.APIKey != "" {
		return instacart.OrderViewForUser(ctx, b.cache, input)
	}
	return nil, nil
}
