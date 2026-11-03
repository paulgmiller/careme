package kroger

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"careme/internal/config"
	"careme/internal/httpretry"
	"careme/internal/providers/kroger/oauth"
	"careme/internal/providers/kroger/products"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

//go:generate go generate ./products ./locations ./oauth ./cart

// krogerTokenManager manages the bearer token and refreshes it if needed
type krogerTokenManager struct {
	token        string
	expiresAt    time.Time
	clientID     string
	clientSecret string
	httpClient   *http.Client
	mu           sync.Mutex
}

func newKrogerTokenManager(clientID, clientSecret string, httpClient *http.Client) *krogerTokenManager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &krogerTokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

// getToken returns a valid token, refreshing if close to expiration
func (m *krogerTokenManager) getToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	// Refresh if less than 1 minute left or not set
	if m.token == "" || now.After(m.expiresAt.Add(-1*time.Minute)) {
		body, err := requestToken(ctx, m.httpClient, m.clientID, m.clientSecret, oauth.AccessTokenFormdataRequestBody{
			GrantType: "client_credentials", Scope: new("product.compact"),
		})
		if err != nil {
			return "", err
		}
		m.token = *body.AccessToken
		m.expiresAt = now.Add(time.Duration(*body.ExpiresIn) * time.Second)
	}
	return m.token, nil
}

// withRetries applies the catalog retry policy while preserving the injected client.
func withRetries(baseClient *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	instrumented := *baseClient
	instrumented.Transport = otelhttp.NewTransport(baseClient.Transport)
	retryClient.HTTPClient = &instrumented
	retryClient.RequestLogHook = httpretry.LogRetry("kroger")
	return retryClient.StandardClient()
}

func newBearerTokenRequestEditor(cfg *config.Config, httpClient *http.Client) func(context.Context, *http.Request) error {
	tokenManager := newKrogerTokenManager(cfg.Kroger.ClientID, cfg.Kroger.ClientSecret, httpClient)

	return func(editorCtx context.Context, req *http.Request) error {
		token, err := tokenManager.getToken(editorCtx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func newProductsClientFromConfig(cfg *config.Config, httpClient *http.Client) (*products.ClientWithResponses, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	requestEditor := newBearerTokenRequestEditor(cfg, httpClient)
	httpClient = withRetries(httpClient)
	productsClient, err := products.NewClientWithResponses("https://api.kroger.com",
		products.WithHTTPClient(httpClient),
		products.WithRequestEditorFn(products.RequestEditorFn(requestEditor)),
	)
	if err != nil {
		return nil, fmt.Errorf("create kroger products client: %w", err)
	}
	return productsClient, nil
}
