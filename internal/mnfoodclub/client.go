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

// Fetch fetches the requested number of pages from a category URL, starting at page 1.
// It preserves listing order and returns no partial results on failure.
func (c *Client) Fetch(ctx context.Context, categoryURL string, pages int) ([]ai.InputIngredient, error) {
	listingURL, err := url.Parse(categoryURL)
	if err != nil {
		return nil, fmt.Errorf("parse category URL %q: %w", categoryURL, err)
	}
	var ingredients []ai.InputIngredient
	for page := 1; page <= pages; page++ {
		query := listingURL.Query()
		query.Set("page", strconv.Itoa(page))
		listingURL.RawQuery = query.Encode()
		items, err := c.fetchPage(ctx, listingURL.String())
		if err != nil {
			return nil, err
		}
		ingredients = append(ingredients, items...)
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
