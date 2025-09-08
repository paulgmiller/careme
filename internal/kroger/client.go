package kroger

import (
	"careme/internal/config"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config cfg.yaml swagger.yaml

// this wasn't in the swagger? try the jsons added next
// OAuth2TokenResponse represents the response from Kroger OAuth2 token endpoint
// LoggingDoer wraps an HttpRequestDoer and logs requests and responses
type LoggingDoer struct {
	Wrapped HttpRequestDoer
}

func (l *LoggingDoer) Do(req *http.Request) (*http.Response, error) {
	fmt.Printf("Kroger Request: %s %s\nHeaders: %v\n", req.Method, req.URL.String(), req.Header)
	resp, err := l.Wrapped.Do(req)
	if err != nil {
		fmt.Printf("Kroger Response Error: %v\n", err)
		return resp, err
	}
	fmt.Printf("Kroger Response: %d %s\n", resp.StatusCode, resp.Status)
	return resp, err
}

type OAuth2TokenResponse struct {
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
	mu           sync.Mutex
}

func NewKrogerTokenManager(clientID, clientSecret string) *KrogerTokenManager {
	return &KrogerTokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
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
		data.Set("scope", "product.compact") //wierd for location?

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(m.clientID, m.clientSecret)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to get token: %s", string(body))
		}

		var tokenResp OAuth2TokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return "", err
		}
		m.token = tokenResp.AccessToken
		m.expiresAt = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return m.token, nil
}

// GetOAuth2Token fetches an access token using client credentials grant
// Deprecated: use KrogerTokenManager instead
func GetOAuth2Token(ctx context.Context, clientID, clientSecret string) (string, error) {
	tm := NewKrogerTokenManager(clientID, clientSecret)
	return tm.GetToken(ctx)
}

func FromConfig(ctx context.Context, cfg *config.Config) (*ClientWithResponses, error) {
	tokenManager := NewKrogerTokenManager(cfg.Kroger.ClientID, cfg.Kroger.ClientSecret)

	// Custom request editor that refreshes token if needed
	requestEditor := func(editorCtx context.Context, req *http.Request) error {
		token, err := tokenManager.GetToken(editorCtx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}

	return NewClientWithResponses("https://api.kroger.com/v1",
		WithRequestEditorFn(requestEditor),
	)
}
