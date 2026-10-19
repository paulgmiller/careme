// Package bigcommerce owns cart handoffs to a configured BigCommerce store.
package bigcommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"careme/internal/httpretry"
	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var (
	ErrUnavailable = errors.New("product unavailable")
	apiPathPattern = regexp.MustCompile(`^/stores/[A-Za-z0-9]+/v3/$`)
	cartIDPattern  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type ClientOptions struct {
	APIPath     string
	AccessToken string
	CartOrigin  string
	Transport   http.RoundTripper
}

type Client struct {
	apiPath     string
	accessToken string
	cartOrigin  string
	httpClient  *http.Client
}

func NewClient(options ClientOptions) (*Client, error) {
	api, err := url.Parse(strings.TrimSpace(options.APIPath))
	if err != nil || api.Scheme != "https" || api.Host != "api.bigcommerce.com" || api.User != nil || api.RawQuery != "" || api.Fragment != "" || !apiPathPattern.MatchString(api.Path) || api.RawPath != "" {
		return nil, fmt.Errorf("bigcommerce requires an HTTPS api.bigcommerce.com/stores/<store_hash>/v3/ API path")
	}
	if strings.TrimSpace(options.AccessToken) == "" {
		return nil, fmt.Errorf("bigcommerce requires an access token")
	}
	origin, err := url.Parse(options.CartOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") || origin.Port() != "" {
		return nil, fmt.Errorf("bigcommerce requires an HTTPS cart origin")
	}
	transport := options.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	retry := retryablehttp.NewClient()
	retry.HTTPClient = &http.Client{Timeout: 15 * time.Second, Transport: otelhttp.NewTransport(transport), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	// Neither cart creation nor single-use redirect generation may be replayed.
	retry.RetryMax = 0
	retry.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retry.Logger = nil
	retry.RequestLogHook = httpretry.LogRetry("bigcommerce")
	return &Client{apiPath: api.String(), accessToken: options.AccessToken, cartOrigin: strings.TrimRight(options.CartOrigin, "/"), httpClient: retry.StandardClient()}, nil
}

// LineItem contains catalog identifiers and package counts only. Prices and names
// are deliberately not overridden with submitted form values.
type LineItem struct {
	ProductID int `json:"product_id"`
	VariantID int `json:"variant_id"`
	Quantity  int `json:"quantity"`
}

type product struct {
	ID                int       `json:"id"`
	Visible           bool      `json:"is_visible"`
	Availability      string    `json:"availability"`
	InventoryTracking string    `json:"inventory_tracking"`
	InventoryLevel    int       `json:"inventory_level"`
	Variants          []variant `json:"variants"`
	Modifiers         []struct {
		Required bool `json:"required"`
	} `json:"modifiers"`
}

type variant struct {
	ID                 int     `json:"id"`
	CalculatedPrice    float64 `json:"calculated_price"`
	InventoryLevel     int     `json:"inventory_level"`
	PurchasingDisabled bool    `json:"purchasing_disabled"`
}

func (c *Client) Resolve(ctx context.Context, id int) (LineItem, error) {
	var result struct {
		Data product `json:"data"`
	}
	status, err := c.request(ctx, http.MethodGet, fmt.Sprintf("catalog/products/%d?include=variants,modifiers", id), nil, &result)
	if status == http.StatusNotFound {
		return LineItem{}, ErrUnavailable
	}
	if err != nil {
		return LineItem{}, fmt.Errorf("resolve BigCommerce product %d: %w", id, err)
	}
	p := result.Data
	if p.ID != id {
		return LineItem{}, fmt.Errorf("bigcommerce product response ID does not match %d", id)
	}
	if !p.Visible || p.Availability != "available" || (p.InventoryTracking == "product" && p.InventoryLevel <= 0) {
		return LineItem{}, ErrUnavailable
	}
	for _, modifier := range p.Modifiers {
		if modifier.Required {
			return LineItem{}, ErrUnavailable
		}
	}
	if len(p.Variants) == 0 {
		return LineItem{}, fmt.Errorf("bigcommerce product %d is missing included variants", id)
	}
	var selected *variant
	for _, v := range p.Variants {
		if v.PurchasingDisabled || (p.InventoryTracking == "variant" && v.InventoryLevel <= 0) {
			continue
		}
		if v.ID <= 0 || v.CalculatedPrice < 0 || math.IsNaN(v.CalculatedPrice) || math.IsInf(v.CalculatedPrice, 0) {
			return LineItem{}, fmt.Errorf("bigcommerce product %d has invalid variant data", id)
		}
		if selected == nil || v.CalculatedPrice < selected.CalculatedPrice {
			selected = &v
		}
	}
	if selected == nil {
		return LineItem{}, ErrUnavailable
	}
	return LineItem{ProductID: id, VariantID: selected.ID, Quantity: 1}, nil
}

func (c *Client) CreateCart(ctx context.Context, items []LineItem) (string, error) {
	body, err := json.Marshal(struct {
		CustomerID int        `json:"customer_id"`
		Items      []LineItem `json:"line_items"`
	}{Items: items})
	if err != nil {
		return "", fmt.Errorf("encode BigCommerce cart: %w", err)
	}
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if _, err := c.request(ctx, http.MethodPost, "carts", body, &result); err != nil {
		return "", fmt.Errorf("create BigCommerce cart: %w", err)
	}
	if !cartIDPattern.MatchString(result.Data.ID) {
		return "", fmt.Errorf("bigcommerce returned an invalid cart ID")
	}
	return result.Data.ID, nil
}

func (c *Client) CartRedirect(ctx context.Context, id string) (string, error) {
	if !cartIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid BigCommerce cart ID")
	}
	var result struct {
		Data struct {
			CartURL string `json:"cart_url"`
		} `json:"data"`
	}
	if _, err := c.request(ctx, http.MethodPost, "carts/"+id+"/redirect_urls", []byte(`{}`), &result); err != nil {
		return "", fmt.Errorf("create BigCommerce cart redirect: %w", err)
	}
	destination, err := url.Parse(result.Data.CartURL)
	if err != nil || destination.Scheme != "https" || destination.User != nil || destination.Scheme+"://"+destination.Host != c.cartOrigin || destination.Path != "/cart.php" || strings.ContainsAny(result.Data.CartURL, "\\\r\n") {
		return "", fmt.Errorf("bigcommerce returned an invalid cart redirect")
	}
	return result.Data.CartURL, nil
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, result any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.apiPath+path, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create BigCommerce request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.accessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request BigCommerce: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	expected := http.StatusOK
	if method == http.MethodPost {
		expected = http.StatusCreated
	}
	if resp.StatusCode != expected {
		return resp.StatusCode, fmt.Errorf("bigcommerce returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read BigCommerce response: %w", err)
	}
	if len(data) > 2<<20 {
		return resp.StatusCode, fmt.Errorf("bigcommerce response is too large")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return resp.StatusCode, fmt.Errorf("decode BigCommerce response: %w", err)
	}
	return resp.StatusCode, nil
}
