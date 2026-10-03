package smithbrothersfarms

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"careme/internal/ai"
)

type Client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *Client { return &Client{httpClient: httpClient} }

func (c *Client) Fetch(ctx context.Context, url string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, Parse)
}

func (c *Client) FetchHarvestBox(ctx context.Context, url, name string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, func(r io.Reader) ([]ai.InputIngredient, error) { return ParseHarvestBox(r, url, name) })
}

func (c *Client) fetch(ctx context.Context, url string, parse func(io.Reader) ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", url, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	items, err := parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	return items, nil
}
