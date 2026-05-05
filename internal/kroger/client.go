package kroger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"careme/internal/config"
	"careme/internal/kroger/products"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

//go:generate go generate ./products ./locations

// this wasn't in the swagger? try the jsons added next
// oAuth2TokenResponse represents the response from Kroger OAuth2 token endpoint
type oAuth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// KrogerTokenManager manages the bearer token and refreshes it if needed
type KrogerTokenManager struct {
	token        string
	expiresAt    time.Time
	clientID     string
	clientSecret string
	httpClient   *http.Client
	mu           sync.Mutex
}

func NewKrogerTokenManager(clientID, clientSecret string, httpClient *http.Client) *KrogerTokenManager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &KrogerTokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

// GetToken returns a valid token, refreshing if close to expiration
func (m *KrogerTokenManager) GetToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	// Refresh if less than 1 minute left or not set
	if m.token == "" || now.After(m.expiresAt.Add(-1*time.Minute)) {
		endpoint := "https://api.kroger.com/v1/connect/oauth2/token"
		data := url.Values{}
		data.Set("grant_type", "client_credentials")
		data.Set("scope", "product.compact") // wierd for location?

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(m.clientID, m.clientSecret)

		resp, err := m.httpClient.Do(req)
		if err != nil {
			return "", err
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				fmt.Printf("Kroger Response Close Error: %v\n", err)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to get token: %s", string(body))
		}

		var tokenResp oAuth2TokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return "", err
		}
		m.token = tokenResp.AccessToken
		m.expiresAt = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return m.token, nil
}

// this would be nice but it logs all retries as errors which sets off alerts.
// var _ retryablehttp.LeveledLogger = slog.Default()
type SlogPrintf struct{}

func (l SlogPrintf) Printf(format string, args ...interface{}) {
	// missing context sadly so no operation id
	slog.With().Info(fmt.Sprintf(format, args...), "source", "retryablehttp")
}

// similiar but less customiszed that bright data.
func withProductRetries(baseClient *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = baseClient
	retryClient.Logger = SlogPrintf{}
	return retryClient.StandardClient()
}

func newBearerTokenRequestEditor(cfg *config.Config, httpClient *http.Client) func(context.Context, *http.Request) error {
	tokenManager := NewKrogerTokenManager(cfg.Kroger.ClientID, cfg.Kroger.ClientSecret, httpClient)

	return func(editorCtx context.Context, req *http.Request) error {
		token, err := tokenManager.GetToken(editorCtx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func NewProductsClientFromConfig(cfg *config.Config, httpClient *http.Client) (*products.ClientWithResponses, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpClient = withProductRetries(httpClient)
	requestEditor := newBearerTokenRequestEditor(cfg, httpClient)
	productsClient, err := products.NewClientWithResponses("https://api.kroger.com",
		products.WithHTTPClient(httpClient),
		products.WithRequestEditorFn(products.RequestEditorFn(requestEditor)),
	)
	if err != nil {
		return nil, fmt.Errorf("create kroger products client: %w", err)
	}
	return productsClient, nil
}
