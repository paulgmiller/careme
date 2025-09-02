package kroger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

// GetOAuth2Token fetches an access token using client credentials grant
func GetOAuth2Token(ctx context.Context, clientID, clientSecret string) (string, error) {
	endpoint := "https://api.kroger.com/v1/connect/oauth2/token"
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "product.compact")

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

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
	return tokenResp.AccessToken, nil
}
