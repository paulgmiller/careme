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
	"careme/internal/httpretry"
	"careme/internal/providers/kroger"
	"careme/internal/routing"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type OrderOptions struct {
	Cache cache.Cache
	Auth  auth.AuthClient
}

// RegisterOrders installs routes for every configured ordering destination.
func (f Factory) RegisterOrders(routes routing.Registrar, options OrderOptions) error {
	if f.config.Kroger.CartTokenKey != "" {
		handler, err := f.newKrogerOrder(options)
		if err != nil {
			return err
		}
		handler.Register(routes)
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
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = &http.Client{Timeout: 15 * time.Second, Transport: otelhttp.NewTransport(http.DefaultTransport)}
	// Cart writes and authorization codes cannot safely be replayed.
	retryClient.RetryMax = 0
	retryClient.Logger = nil
	retryClient.RequestLogHook = httpretry.LogRetry("kroger")
	httpClient := retryClient.StandardClient()
	client := &kroger.CartClient{
		ClientID: cfg.Kroger.ClientID, ClientSecret: cfg.Kroger.ClientSecret,
		RedirectURI:  cfg.ResolvedPublicOrigin() + "/kroger/callback",
		HTTPClient:   httpClient,
		CatalogToken: kroger.NewKrogerTokenManager(cfg.Kroger.ClientID, cfg.Kroger.ClientSecret, httpClient),
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

// OrderViewBuilder binds provider-owned transfer reads to the application's cache.
type OrderViewBuilder struct {
	factory Factory
	cache   cache.Cache
}

func (f Factory) NewOrderViewBuilder(c cache.Cache) *OrderViewBuilder {
	return &OrderViewBuilder{factory: f, cache: c}
}

func (b *OrderViewBuilder) OrderView(ctx context.Context, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if kroger.NewIdentityProvider().IsID(input.Location.ID) && b.factory.config.Kroger.CartTokenKey != "" {
		return kroger.OrderViewForUser(ctx, b.cache, input)
	}
	return nil, nil
}
