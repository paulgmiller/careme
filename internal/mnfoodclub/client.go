// Package mnfoodclub extracts ingredients from MNFood.Club category listings.
package mnfoodclub

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"careme/internal/ai"
)

// Client fetches public category listings using the supplied HTTP client.
type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

// FetchIngredients fetches pages 1–3 of produce, followed by pages 1–3 of meat.
// It preserves listing order and returns no partial results on failure.
func (c *Client) FetchIngredients(ctx context.Context) ([]ai.InputIngredient, error) {
	var ingredients []ai.InputIngredient
	for _, categoryURL := range []string{
		"https://mnfood.club/shop-all/produce/?sort=bestselling",
		"https://mnfood.club/shop-all/meat/",
	} {
		listingURL, err := url.Parse(categoryURL)
		if err != nil {
			return nil, fmt.Errorf("parse category URL: %w", err)
		}
		for page := 1; page <= 3; page++ {
			query := listingURL.Query()
			query.Set("page", strconv.Itoa(page))
			listingURL.RawQuery = query.Encode()
			items, err := c.fetchPage(ctx, listingURL.String())
			if err != nil {
				return nil, err
			}
			ingredients = append(ingredients, items...)
		}
	}
	return ingredients, nil
}

func (c *Client) fetchPage(ctx context.Context, pageURL string) ([]ai.InputIngredient, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", pageURL, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", pageURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", pageURL, resp.StatusCode)
	}
	ingredients, err := Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", pageURL, err)
	}
	return ingredients, nil
}
