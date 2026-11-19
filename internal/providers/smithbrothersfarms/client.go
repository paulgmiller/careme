package smithbrothersfarms

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type client struct{ httpClient *retryablehttp.Client }

func NewClient(httpClient *http.Client) *client {
	base := *httpClient
	base.Transport = otelhttp.NewTransport(base.Transport)
	base.Timeout = 30 * time.Second
	if httpClient.Timeout > 0 && httpClient.Timeout < base.Timeout {
		base.Timeout = httpClient.Timeout
	}
	retries := retryablehttp.NewClient()
	retries.HTTPClient = &base
	retries.Logger = nil
	retries.RetryMax = 2
	retries.RetryWaitMin = 100 * time.Millisecond
	retries.RetryWaitMax = time.Second
	retries.RequestLogHook = httpretry.LogRetry("smithbrothersfarms")
	retries.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return &client{httpClient: retries}
}

func (c *client) Fetch(ctx context.Context, url string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, parse)
}

func (c *client) FetchHarvestBox(ctx context.Context, url, name string) ([]ai.InputIngredient, error) {
	return c.fetch(ctx, url, func(r io.Reader) ([]ai.InputIngredient, error) { return parseHarvestBox(r, url, name) })
}

func (c *client) fetch(ctx context.Context, url string, parse func(io.Reader) ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	req, err := retryablehttp.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", url, err)
	}
	// Read the complete body inside the retry boundary. HTTP Do alone only
	// waits for headers, so a timeout or truncation during parsing escaped retries.
	var body []byte
	req.SetResponseHandler(func(resp *http.Response) error {
		defer func() {
			_ = resp.Body.Close()
			resp.Body = http.NoBody
		}()
		if resp.StatusCode != http.StatusOK {
			return nil
		}
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read %s: %w", url, err)
		}
		return nil
	})
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	items, err := parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	return items, nil
}
