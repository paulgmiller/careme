package kroger

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"careme/internal/providers/kroger/cart"
	"careme/internal/providers/kroger/oauth"
	"careme/internal/providers/kroger/products"
)

const krogerAPIOrigin = "https://api.kroger.com"

var errProductUnavailable = errors.New("kroger product unavailable")

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
	CatalogToken                        *krogerTokenManager
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

// productUPC resolves a catalog product at the intended store to one UPC, independently of inventory.
func (c *CartClient) productUPC(ctx context.Context, productID, locationID string) (string, error) {
	if c.CatalogToken == nil {
		return "", fmt.Errorf("kroger product token unavailable")
	}
	token, err := c.CatalogToken.getToken(ctx)
	if err != nil {
		return "", fmt.Errorf("authorize Kroger product lookup: %w", err)
	}
	client, err := products.NewClientWithResponses(krogerAPIOrigin, products.WithHTTPClient(c.HTTPClient))
	if err != nil {
		return "", fmt.Errorf("create Kroger products client: %w", err)
	}
	resp, err := client.ProductGetIDWithResponse(ctx, productID, &products.ProductGetIDParams{FilterLocationId: &locationID}, func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("look up Kroger product %q: %w", productID, err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return "", fmt.Errorf("%w: %q", errProductUnavailable, productID)
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("kroger product %q lookup returned %d", productID, resp.StatusCode())
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return "", fmt.Errorf("decode Kroger product %q: missing product data", productID)
	}
	if resp.JSON200.Data.Items != nil {
		for _, item := range *resp.JSON200.Data.Items {
			if upc := strings.TrimSpace(stringValue(item.ItemId)); upc != "" {
				return upc, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %q has no UPC", errProductUnavailable, productID)
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
