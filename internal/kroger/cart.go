package kroger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const krogerAPIOrigin = "https://api.kroger.com"

var ErrProductUnavailable = errors.New("kroger product unavailable")

type CartToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type CartItem struct {
	UPC      string `json:"upc"`
	Quantity int    `json:"quantity"`
}

type CartClient struct {
	ClientID, ClientSecret, RedirectURI string
	HTTPClient                          *http.Client
	CatalogToken                        *KrogerTokenManager
}

func (c *CartClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *CartClient) AuthorizationURL(state string) string {
	query := url.Values{
		"response_type": {"code"}, "client_id": {c.ClientID},
		"redirect_uri": {c.RedirectURI}, "scope": {"cart.basic:write"}, "state": {state},
	}
	return krogerAPIOrigin + "/v1/connect/oauth2/authorize?" + query.Encode()
}

func (c *CartClient) tokenRequest(ctx context.Context, values url.Values) (CartToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, krogerAPIOrigin+"/v1/connect/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return CartToken{}, err
	}
	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return CartToken{}, fmt.Errorf("request Kroger token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return CartToken{}, fmt.Errorf("kroger token request returned %d", resp.StatusCode)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return CartToken{}, fmt.Errorf("decode Kroger token: %w", err)
	}
	if body.AccessToken == "" || body.ExpiresIn <= 0 {
		return CartToken{}, fmt.Errorf("incomplete Kroger token response")
	}
	return CartToken{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)}, nil
}

func (c *CartClient) Exchange(ctx context.Context, code string) (CartToken, error) {
	token, err := c.tokenRequest(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.RedirectURI}})
	if err != nil {
		return CartToken{}, err
	}
	if token.RefreshToken == "" {
		return CartToken{}, fmt.Errorf("kroger did not provide a refresh token")
	}
	return token, nil
}

func (c *CartClient) Refresh(ctx context.Context, refreshToken string) (CartToken, error) {
	result, err := c.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return CartToken{}, err
	}
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}
	return result, nil
}

// ProductUPC resolves a catalog product at the intended store to one sellable UPC.
func (c *CartClient) ProductUPC(ctx context.Context, productID, locationID string) (string, error) {
	if c.CatalogToken == nil {
		return "", fmt.Errorf("kroger product token unavailable")
	}
	token, err := c.CatalogToken.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("authorize Kroger product lookup: %w", err)
	}
	endpoint := krogerAPIOrigin + "/v1/products/" + url.PathEscape(productID) + "?filter.locationId=" + url.QueryEscape(locationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("look up Kroger product %q: %w", productID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%w: %q", ErrProductUnavailable, productID)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kroger product %q lookup returned %d", productID, resp.StatusCode)
	}
	var body struct {
		Data struct {
			Items []struct {
				ItemID    string `json:"itemId"`
				Inventory struct {
					StockLevel string `json:"stockLevel"`
				} `json:"inventory"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("decode Kroger product %q: %w", productID, err)
	}
	for _, item := range body.Data.Items {
		if item.Inventory.StockLevel == "TEMPORARILY_OUT_OF_STOCK" {
			continue
		}
		if strings.TrimSpace(item.ItemID) != "" {
			return strings.TrimSpace(item.ItemID), nil
		}
	}
	return "", fmt.Errorf("%w: %q has no available UPC", ErrProductUnavailable, productID)
}

func (c *CartClient) Add(ctx context.Context, token string, items []CartItem) error {
	if len(items) == 0 {
		return fmt.Errorf("cart has no items")
	}
	body, err := json.Marshal(struct {
		Items []CartItem `json:"items"`
	}{Items: items})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, krogerAPIOrigin+"/v1/cart/add", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("add Kroger cart items: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kroger cart returned %d", resp.StatusCode)
	}
	return nil
}
