package providerregistry

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	ordertypes "careme/internal/orders/types"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/httpretry"
	"careme/internal/providers/bigcommerce"
	"careme/internal/providers/instacart"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
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
	if f.config.Instacart.APIKey != "" {
		client, err := instacart.NewClient(instacart.ClientOptions{APIKey: f.config.Instacart.APIKey, BaseURL: f.config.Instacart.BaseURL})
		if err != nil {
			return err
		}
		handler, err := instacart.NewOrderHandler(instacart.OrderOptions{Cache: options.Cache, Client: client, Origin: f.config.ResolvedPublicOrigin(), Auth: options.Auth})
		if err != nil {
			return err
		}
		handler.Register(routes)
	}
	if f.config.MNFoodClub.CartEnable {
		client, err := bigcommerce.NewClient(bigcommerce.ClientOptions{APIPath: f.config.MNFoodClub.APIPath, AccessToken: f.config.MNFoodClub.AccessToken, CartOrigin: f.config.MNFoodClub.CartOrigin})
		if err != nil {
			return err
		}
		handler, err := bigcommerce.NewOrderHandler(bigcommerce.OrderOptions{Cache: options.Cache, Client: client, Auth: options.Auth, LocationPrefix: mnfoodclub.LocationIDPrefix})
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

// OrderViews builds each configured destination's form and return presentation.
func (f Factory) OrderViews(input ordertypes.OrderViewInput) ([]*ordertypes.OrderView, error) {
	var views []*ordertypes.OrderView
	if kroger.NewIdentityProvider().IsID(input.Location.ID) && f.config.Kroger.CartTokenKey != "" {
		view, err := kroger.OrderView(input)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if f.config.MNFoodClub.CartEnable && mnfoodclub.NewIdentityProvider().IsID(input.Location.ID) {
		view, err := bigcommerce.OrderView(input, "MNFoodClub")
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if f.config.Instacart.APIKey != "" {
		view, err := instacart.OrderView(input)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
