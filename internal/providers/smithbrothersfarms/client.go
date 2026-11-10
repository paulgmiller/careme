package smithbrothersfarms

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"careme/internal/ai"
)

type client struct{ httpClient *http.Client }

func NewClient(httpClient *http.Client) *client { return &client{httpClient: httpClient} }

func (c *client) Fetch(ctx context.Context, url string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, parse)
}

func (c *client) FetchHarvestBox(ctx context.Context, url, name string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, func(r io.Reader) ([]ai.InputIngredient, error) { return parseHarvestBox(r, url, name) })
}

func (c *client) fetch(ctx context.Context, url string, parse func(io.Reader) ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) {
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
