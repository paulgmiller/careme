package providerregistry

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/httpretry"
	"careme/internal/ordering"
	"careme/internal/providers/kroger"
	"careme/internal/routing"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type OrderingOptions struct {
	Cache  cache.Cache
	Auth   auth.AuthClient
	Resume func(w http.ResponseWriter, r *http.Request, providerID, userID, hash string)
}

// Ordering assembles enabled ordering providers and their connection routes.
type Ordering struct {
	Service *ordering.Service
	kroger  *kroger.OrderingAdapter
}

func (f Factory) NewOrdering(options OrderingOptions) (*Ordering, error) {
	result := &Ordering{}
	var providers []ordering.Provider
	cfg := f.config
	if cfg.Kroger.CartTokenKey != "" {
		if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
			return nil, fmt.Errorf("kroger cart requires client ID and secret")
		}
		key, err := base64.StdEncoding.DecodeString(cfg.Kroger.CartTokenKey)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("KROGER_CART_TOKEN_KEY must be a base64-encoded 32-byte key")
		}
		if options.Resume == nil {
			return nil, fmt.Errorf("ordering requires a resume handler")
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
		adapter, err := kroger.NewOrderingAdapter(kroger.OrderingOptions{
			Cache: options.Cache, Client: client, TokenKey: key,
			Origin: cfg.ResolvedPublicOrigin(), Auth: options.Auth,
			Resume: func(w http.ResponseWriter, r *http.Request, userID, hash string) {
				options.Resume(w, r, "kroger", userID, hash)
			},
		})
		if err != nil {
			return nil, fmt.Errorf("create Kroger ordering adapter: %w", err)
		}
		result.kroger = adapter
		providers = append(providers, adapter)
	}
	result.Service = ordering.New(options.Cache, providers)
	return result, nil
}

func (o *Ordering) Register(routes routing.Registrar) {
	if o.kroger != nil {
		routes.HandleFunc("GET /kroger/callback", o.kroger.Callback)
	}
}
