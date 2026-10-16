package providerregistry

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/httpretry"
	"careme/internal/providers/kroger"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type KrogerOrderOptions struct {
	Cache cache.Cache
	Auth  auth.AuthClient
}

func (f Factory) NewKrogerOrder(options KrogerOrderOptions) (*kroger.OrderHandler, error) {
	cfg := f.config
	if cfg.Kroger.CartTokenKey != "" {
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
	return nil, nil
}
