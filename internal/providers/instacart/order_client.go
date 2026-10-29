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

	"careme/internal/config"
	"careme/internal/httpretry"
	ordertypes "careme/internal/orders/types"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	ProductionURL  = "https://connect.instacart.com"
	DevelopmentURL = "https://connect.dev.instacart.tools"
)

// OrderClient creates shoppable lists using the server-side Instacart API key.
type OrderClient struct {
	apiKey     string
	baseURL    string
	origin     string
	httpClient *http.Client
}

func NewOrderClient(cfg config.InstacartConfig, origin string, httpClient *http.Client) (*OrderClient, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = ProductionURL
	}
	if baseURL != ProductionURL && baseURL != DevelopmentURL {
		return nil, fmt.Errorf("instacart base URL must be the production or development endpoint")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("instacart requires an API key")
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("instacart requires a valid public origin")
	}
	retry := retryablehttp.NewClient()
	retry.HTTPClient = httpClient
	// Link creation has no documented idempotency key. Never replay POSTs.
	retry.RetryMax = 0
	retry.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retry.Logger = nil
	retry.RequestLogHook = httpretry.LogRetry("instacart")
	client := retry.StandardClient()
	client.Timeout = httpClient.Timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &OrderClient{apiKey: cfg.APIKey, baseURL: baseURL, origin: strings.TrimRight(origin, "/"), httpClient: client}, nil
}

func (c *OrderClient) create(ctx context.Context, input ordertypes.TransferRequest) (string, error) {
	payload := shoppingList{Title: "Careme shopping list", LinkType: "shopping_list", ExpiresIn: 30, LandingPage: landingPage{LinkbackURL: c.origin + input.ReturnURL}}
	for _, item := range input.Items {
		display := item.Name
		if item.Needed != "" {
			display += " — " + item.Needed
		}
		payload.Items = append(payload.Items, lineItem{Name: item.Name, DisplayText: display, Measurements: parseMeasurement(item.Needed)})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Instacart list: %w", err)
	}
	return c.createLink(ctx, body)
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

func (c *OrderClient) createLink(ctx context.Context, body []byte) (string, error) {
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
		return "", fmt.Errorf("instacart link request returned %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return "", fmt.Errorf("read Instacart link: %w", err)
	}
	if len(body) > 1<<20 {
		return "", fmt.Errorf("instacart link response is too large")
	}
	var result struct {
		URL string `json:"products_link_url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode Instacart link: %w", err)
	}
	if !validLink(result.URL) {
		return "", fmt.Errorf("instacart returned an invalid shopping link")
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
