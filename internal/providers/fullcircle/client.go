// Package fullcircle imports Full Circle's public grocery catalog.
package fullcircle

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const baseURL = "https://www.fullcircle.com"

type client struct{ httpClient *http.Client }

// NewClient preserves the caller's transport and adds tracing and bounded retries.
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
	retries.RequestLogHook = httpretry.LogRetry("fullcircle")
	retries.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return &client{httpClient: retries.StandardClient()}
}

// product matches the JSON loaded by the public farm-product-list page.
// SoldOut is null in the anonymous catalog; availability is not delivery-specific.
type product struct {
	ListingID    int      `json:"ProductListingId"`
	Name         string   `json:"ProductName"`
	Subtitle     string   `json:"ProductSubTitle"`
	Vendor       string   `json:"VendorName"`
	Price        *float32 `json:"Price"`
	RegularPrice *float32 `json:"RegularPrice"`
	SoldOut      bool     `json:"SoldOut"`
}

func (c *client) Fetch(ctx context.Context, endpoint, category string) ([]ai.InputIngredient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Full Circle request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Full Circle %s: %w", category, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch Full Circle %s: HTTP %d", category, resp.StatusCode)
	}
	var products []product
	if err := json.NewDecoder(resp.Body).Decode(&products); err != nil {
		return nil, fmt.Errorf("decode Full Circle %s: %w", category, err)
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("empty Full Circle %s catalog", category)
	}
	items := make([]ai.InputIngredient, 0, len(products))
	for _, p := range products {
		if p.SoldOut {
			continue
		}
		name := cleanText(p.Name)
		if p.ListingID <= 0 || name == "" || p.Price == nil || *p.Price < 0 {
			return nil, fmt.Errorf("invalid Full Circle %s product (listing %d): ID, name, and nonnegative price are required", category, p.ListingID)
		}
		item := ai.InputIngredient{
			ProductID:   "fullcircle_" + strconv.Itoa(p.ListingID),
			Description: name, Brand: cleanText(p.Vendor), Size: cleanText(p.Subtitle),
			PriceRegular: p.Price, Categories: []string{category},
		}
		if p.RegularPrice != nil && *p.RegularPrice > *p.Price {
			item.PriceRegular, item.PriceSale = p.RegularPrice, p.Price
		}
		items = append(items, item)
	}
	return items, nil
}

func cleanText(s string) string { return strings.TrimSpace(html.UnescapeString(s)) }
