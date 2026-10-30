package kroger

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"careme/internal/providers/kroger/cart"
	"careme/internal/providers/kroger/oauth"
)

const krogerAPIOrigin = "https://api.kroger.com"

type cartToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type cartItem struct {
	UPC      string `json:"upc"`
	Quantity int    `json:"quantity"`
}

type CartClient struct {
	ClientID, ClientSecret, RedirectURI string
	HTTPClient                          *http.Client
}

func (c *CartClient) authorizationURL(state, banner string) (string, error) {
	req, err := oauth.NewAuthorizationCodeRequest(krogerAPIOrigin, &oauth.AuthorizationCodeParams{
		Scope: "cart.basic:write", ClientId: c.ClientID, RedirectUri: c.RedirectURI,
		ResponseType: "code", State: &state, Banner: &banner,
	})
	if err != nil {
		return "", fmt.Errorf("create Kroger authorization URL: %w", err)
	}
	return req.URL.String(), nil
}

func (c *CartClient) tokenRequest(ctx context.Context, body oauth.AccessTokenFormdataRequestBody) (cartToken, error) {
	token, err := requestToken(ctx, c.HTTPClient, c.ClientID, c.ClientSecret, body)
	if err != nil {
		return cartToken{}, err
	}
	return cartToken{
		AccessToken: *token.AccessToken, RefreshToken: stringValue(token.RefreshToken),
		ExpiresAt: time.Now().Add(time.Duration(*token.ExpiresIn) * time.Second),
	}, nil
}

func (c *CartClient) exchange(ctx context.Context, code string) (cartToken, error) {
	token, err := c.tokenRequest(ctx, oauth.AccessTokenFormdataRequestBody{GrantType: "authorization_code", Code: &code, RedirectUri: &c.RedirectURI})
	if err != nil {
		return cartToken{}, err
	}
	if token.RefreshToken == "" {
		return cartToken{}, fmt.Errorf("kroger did not provide a refresh token")
	}
	return token, nil
}

func (c *CartClient) refresh(ctx context.Context, refreshToken string) (cartToken, error) {
	result, err := c.tokenRequest(ctx, oauth.AccessTokenFormdataRequestBody{GrantType: "refresh_token", RefreshToken: &refreshToken})
	if err != nil {
		return cartToken{}, err
	}
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}
	return result, nil
}

func (c *CartClient) add(ctx context.Context, token string, items []cartItem) error {
	if len(items) == 0 {
		return fmt.Errorf("cart has no items")
	}
	client, err := cart.NewClientWithResponses(krogerAPIOrigin, cart.WithHTTPClient(c.HTTPClient))
	if err != nil {
		return fmt.Errorf("create Kroger cart client: %w", err)
	}
	generatedItems := make([]cart.Item, len(items))
	for i, item := range items {
		generatedItems[i] = cart.Item{Upc: item.UPC, Quantity: item.Quantity}
	}
	resp, err := client.AddToCartWithResponse(ctx, cart.AddToCartJSONRequestBody{Items: generatedItems}, func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return fmt.Errorf("add Kroger cart items: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("kroger cart returned %d", resp.StatusCode())
	}
	return nil
}
