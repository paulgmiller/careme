package kroger

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"careme/internal/providers/kroger/oauth"
)

func requestToken(ctx context.Context, httpClient *http.Client, clientID, clientSecret string, body oauth.AccessTokenFormdataRequestBody) (*oauth.AuthorizationCodeResponse, error) {
	client, err := oauth.NewClientWithResponses(krogerAPIOrigin, oauth.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create Kroger OAuth client: %w", err)
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret))
	resp, err := client.AccessTokenWithFormdataBodyWithResponse(ctx, &oauth.AccessTokenParams{Authorization: auth}, body)
	if err != nil {
		return nil, fmt.Errorf("request Kroger token: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("kroger token request returned %d", resp.StatusCode())
	}
	token := resp.JSON200
	if token == nil || token.AccessToken == nil || *token.AccessToken == "" || token.ExpiresIn == nil || *token.ExpiresIn <= 0 {
		return nil, fmt.Errorf("incomplete Kroger token response")
	}
	return token, nil
}
