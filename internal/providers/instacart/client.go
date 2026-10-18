// Package instacart owns shopping-list handoffs to Instacart Marketplace.
package instacart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/httpretry"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	ProductionURL  = "https://connect.instacart.com"
	DevelopmentURL = "https://connect.dev.instacart.tools"
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

type ClientOptions struct {
	APIKey  string
	BaseURL string
	// Transport is instrumented without replacing the caller's transport.
	Transport http.RoundTripper
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL := strings.TrimRight(options.BaseURL, "/")
	if baseURL == "" {
		baseURL = ProductionURL
	}
	if baseURL != ProductionURL && baseURL != DevelopmentURL {
		return nil, fmt.Errorf("Instacart base URL must be the production or development endpoint")
	}
	if strings.TrimSpace(options.APIKey) == "" {
		return nil, fmt.Errorf("Instacart requires an API key")
	}
	transport := options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	retry := retryablehttp.NewClient()
	retry.HTTPClient = &http.Client{Timeout: 15 * time.Second, Transport: otelhttp.NewTransport(transport), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	// Link creation has no documented idempotency key. Do not replay POSTs.
	retry.RetryMax = 0
	retry.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retry.Logger = nil
	retry.RequestLogHook = httpretry.LogRetry("instacart")
	return &Client{apiKey: options.APIKey, baseURL: baseURL, httpClient: retry.StandardClient()}, nil
}

type measurement struct {
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
}

type lineItem struct {
	Name         string        `json:"name"`
	DisplayText  string        `json:"display_text"`
	Measurements []measurement `json:"line_item_measurements,omitempty"`
}

type shoppingList struct {
	Title       string      `json:"title"`
	LinkType    string      `json:"link_type"`
	ExpiresIn   int         `json:"expires_in"`
	Items       []lineItem  `json:"line_items"`
	LandingPage landingPage `json:"landing_page_configuration"`
}

type landingPage struct {
	LinkbackURL string `json:"partner_linkback_url"`
}

func (c *Client) createLink(ctx context.Context, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/idp/v1/products/products_link", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create Instacart request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create Instacart link: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Instacart link request returned %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return "", fmt.Errorf("read Instacart link: %w", err)
	}
	if len(body) > 1<<20 {
		return "", fmt.Errorf("Instacart link response is too large")
	}
	var result struct {
		URL string `json:"products_link_url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode Instacart link: %w", err)
	}
	if !validLink(result.URL) {
		return "", fmt.Errorf("Instacart returned an invalid shopping link")
	}
	return result.URL, nil
}

func validLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (host == "instacart.com" || strings.HasSuffix(host, ".instacart.com")) && !strings.ContainsAny(raw, "\\\r\n")
}
