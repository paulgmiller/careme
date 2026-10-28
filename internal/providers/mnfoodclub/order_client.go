package mnfoodclub

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

	"careme/internal/config"
	"careme/internal/httpretry"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type requestedItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id,omitempty"`
	Quantity  int    `json:"quantity"`
}

type identityHints struct {
	Emails []string `json:"emails,omitempty"`
}

type handoffRequest struct {
	Items         []requestedItem `json:"items"`
	IdentityHints identityHints   `json:"identity_hints"`
}

type itemResult struct {
	ProductID string `json:"product_id"`
	Accepted  int    `json:"accepted_quantity"`
	Rejected  int    `json:"rejected_quantity"`
	Reason    string `json:"rejection_reason,omitempty"`
}

type handoffResponse struct {
	TransferID string       `json:"transfer_id"`
	Status     string       `json:"status"`
	CartURL    string       `json:"cart_url"`
	UserID     string       `json:"user_id,omitempty"`
	Items      []itemResult `json:"items,omitempty"`
}

// OrderClient calls the proposed partner API with credentials separate from BigCommerce.
type OrderClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
}

func NewOrderClient(cfg config.MNFoodClubConfig, baseClient *http.Client) (*OrderClient, error) {
	base, err := url.Parse(cfg.OrderAPIURL)
	if err != nil || !validOrderURL(base) || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("MNFOODCLUB_ORDER_API_URL must be an absolute HTTPS URL (HTTP localhost is allowed for testing)")
	}
	if strings.TrimSpace(cfg.OrderAccessToken) == "" {
		return nil, fmt.Errorf("MN Food Club ordering requires MNFOODCLUB_ORDER_ACCESS_TOKEN")
	}
	instrumented := *baseClient
	instrumented.Timeout = 15 * time.Second
	instrumented.Transport = otelhttp.NewTransport(baseClient.Transport)
	instrumented.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	retry := retryablehttp.NewClient()
	retry.HTTPClient = &instrumented
	// Transfers are never automatically resubmitted, including after ambiguous failures.
	retry.RetryMax = 0
	retry.Logger = nil
	retry.RequestLogHook = httpretry.LogRetry("mnfoodclub-order")
	return &OrderClient{endpoint: strings.TrimRight(base.String(), "/") + "/v1/cart-transfers", token: cfg.OrderAccessToken, httpClient: retry.StandardClient()}, nil
}

func validOrderURL(u *url.URL) bool {
	if u == nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

func (c *OrderClient) create(ctx context.Context, key string, input handoffRequest) (handoffResponse, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return handoffResponse{}, fmt.Errorf("encode MN Food Club transfer: %w", err)
	}
	result, err := c.request(ctx, http.MethodPost, c.endpoint, key, body)
	if err != nil {
		return handoffResponse{}, err
	}
	if result.Status == "applied" {
		quantities := make(map[string]int, len(input.Items))
		for _, item := range input.Items {
			quantities[item.ProductID] = item.Quantity
		}
		for _, item := range result.Items {
			if quantity, ok := quantities[item.ProductID]; !ok || item.Accepted+item.Rejected != quantity {
				return handoffResponse{}, fmt.Errorf("MN Food Club returned inconsistent quantities for product %q", item.ProductID)
			}
			delete(quantities, item.ProductID)
		}
		if len(quantities) != 0 {
			return handoffResponse{}, fmt.Errorf("MN Food Club omitted product results")
		}
	}
	return result, nil
}

func (c *OrderClient) status(ctx context.Context, id string) (handoffResponse, error) {
	if id == "" {
		return handoffResponse{}, fmt.Errorf("MN Food Club transfer has no partner ID yet")
	}
	result, err := c.request(ctx, http.MethodGet, c.endpoint+"/"+url.PathEscape(id), "", nil)
	if err == nil && result.TransferID != id {
		return handoffResponse{}, fmt.Errorf("MN Food Club returned a different transfer ID")
	}
	return result, err
}

func (c *OrderClient) request(ctx context.Context, method, endpoint, key string, body []byte) (handoffResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return handoffResponse{}, fmt.Errorf("create MN Food Club request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return handoffResponse{}, fmt.Errorf("MN Food Club transfer request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return handoffResponse{}, fmt.Errorf("MN Food Club %s transfer returned HTTP %d", method, resp.StatusCode)
	}
	var result handoffResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return handoffResponse{}, fmt.Errorf("decode MN Food Club transfer: %w", err)
	}
	if err := result.validate(); err != nil {
		return handoffResponse{}, err
	}
	return result, nil
}

func (r handoffResponse) validate() error {
	u, err := url.Parse(r.CartURL)
	if err != nil || !validOrderURL(u) || strings.TrimSpace(r.TransferID) == "" {
		return fmt.Errorf("MN Food Club returned an invalid transfer ID or cart URL")
	}
	switch r.Status {
	case "pending_login":
		return nil
	case "applied":
		if r.UserID == "" || len(r.Items) == 0 {
			return fmt.Errorf("MN Food Club returned incomplete applied results")
		}
		seen := make(map[string]bool, len(r.Items))
		for _, item := range r.Items {
			if item.ProductID == "" || seen[item.ProductID] || item.Accepted < 0 || item.Rejected < 0 || item.Accepted+item.Rejected <= 0 || (item.Rejected > 0 && item.Reason == "") {
				return fmt.Errorf("MN Food Club returned invalid product results")
			}
			seen[item.ProductID] = true
		}
		return nil
	default:
		return fmt.Errorf("unknown MN Food Club transfer status %q", r.Status)
	}
}
