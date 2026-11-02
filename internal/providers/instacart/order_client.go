// Package instacart owns shopping-list handoffs to Instacart Marketplace.
package instacart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/config"
	ordertypes "careme/internal/orders/types"
)

const (
	productionURL  = "https://connect.instacart.com"
	developmentURL = "https://connect.dev.instacart.tools"
)

// orderClient creates shoppable lists using the server-side Instacart API key.
type orderClient struct {
	apiKey     string
	baseURL    string
	origin     string
	httpClient *http.Client
}

func NewOrderClient(cfg config.InstacartConfig, origin string, httpClient *http.Client) (*orderClient, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = productionURL
	}
	if baseURL != productionURL && baseURL != developmentURL {
		return nil, fmt.Errorf("instacart base URL must be the production or development endpoint")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("instacart requires an API key")
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("instacart requires a valid public origin")
	}
	// Local Careme origins always use Instacart development, including when
	// an existing environment setting explicitly selects production.
	if strings.EqualFold(u.Hostname(), "localhost") || net.ParseIP(u.Hostname()).IsLoopback() {
		baseURL = developmentURL
	}
	return &orderClient{apiKey: cfg.APIKey, baseURL: baseURL, origin: strings.TrimRight(origin, "/"), httpClient: httpClient}, nil
}

func (c *orderClient) create(ctx context.Context, input ordertypes.TransferRequest) (string, error) {
	returnURL := c.origin + input.ReturnURL
	payload := shoppingList{
		Title:        fmt.Sprintf("Careme shopping list — %s — %s", input.Location.Name, time.Now().Format(time.DateOnly)),
		LinkType:     "shopping_list",
		ExpiresIn:    7,
		Instructions: []string{returnURL},
		LandingPage:  landingPage{LinkbackURL: returnURL},
	}
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
	Title        string      `json:"title"`
	LinkType     string      `json:"link_type"`
	ExpiresIn    int         `json:"expires_in"`
	Instructions []string    `json:"instructions"`
	Items        []lineItem  `json:"line_items"`
	LandingPage  landingPage `json:"landing_page_configuration"`
}

type landingPage struct {
	LinkbackURL string `json:"partner_linkback_url"`
	// TODO: Revisit enable_pantry_items when supported for shopping_list links; currently recipe-only.
}

// createLink uses Instacart’s Create shopping list page API.
// https://docs.instacart.com/developer_platform_api/api/products/create_shopping_list_page
func (c *orderClient) createLink(ctx context.Context, body []byte) (string, error) {
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
	if err := validateLink(result.URL, c.baseURL); err != nil {
		// Query strings and userinfo can contain credentials or handoff tokens.
		reason := strings.ReplaceAll(err.Error(), c.apiKey, "[redacted]")
		return "", fmt.Errorf("instacart returned an invalid shopping link (API %s): %s", c.baseURL, reason)
	}
	return result.URL, nil
}

func validateLink(raw, baseURL string) error {
	if raw == "" {
		return fmt.Errorf("products_link_url is missing or empty")
	}
	if strings.ContainsAny(raw, "\\\r\n") {
		return fmt.Errorf("products_link_url contains a backslash or line break")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse errors include the input URL; do not propagate them into logs.
		return fmt.Errorf("products_link_url is malformed")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" {
		return fmt.Errorf("products_link_url must use HTTPS (scheme %q, host %q)", u.Scheme, host)
	}
	if u.User != nil {
		return fmt.Errorf("products_link_url contains credentials (host %q)", host)
	}
	if u.Port() != "" {
		return fmt.Errorf("products_link_url contains explicit port %q (host %q)", u.Port(), host)
	}
	developmentHost := baseURL == developmentURL && host == "customers.dev.instacart.tools"
	if host != "instacart.com" && !strings.HasSuffix(host, ".instacart.com") && !developmentHost {
		return fmt.Errorf("products_link_url has unapproved host %q", host)
	}
	return nil
}
