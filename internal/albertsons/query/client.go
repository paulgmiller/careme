package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	vegatables = "GR-C-categ-8c62c848"
	fruit      = "GR-C-categ-a8eea474"
	seafood    = "GR-C-Categ-6090cd27"
	meat       = "GR-MeatF-fffc8662"
)

const (
	DefaultSearchBaseURL = "https://www.safeway.com"
	defaultSearchPath    = "/abs/pub/xapi/wcax/pathway/search"
	defaultSearchRows    = 60   // how high can we go.
	defaultSearchWidget  = meat // need to get more categories.
	defaultSearchChannel = "instore"
	defaultSearchUser    = "G"
)

type SearchClient struct {
	baseURL         string
	subscriptionKey string
	reese84         string
	visitorID       string
	httpClient      *http.Client
}

type SearchClientConfig struct {
	BaseURL         string
	SubscriptionKey string
	Reese84         string
	VisitorID       string
	HTTPClient      *http.Client
}

type SearchOptions struct {
	Query    string
	Start    int
	Rows     int
	Sort     string
	WidgetID string // category
}

type SearchResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
}

func (r *SearchResponse) DecodeJSON(dest any) error {
	if len(r.Body) == 0 {
		return errors.New("response body is empty")
	}
	if err := json.Unmarshal(r.Body, dest); err != nil {
		return fmt.Errorf("decode json response: %w", err)
	}
	return nil
}

func NewSearchClient(cfg SearchClientConfig) (*SearchClient, error) {
	subscriptionKey := strings.TrimSpace(cfg.SubscriptionKey)
	if subscriptionKey == "" {
		return nil, errors.New("subscription key is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultSearchBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	visitorID := strings.TrimSpace(cfg.VisitorID)
	if visitorID == "" {
		visitorID = uuid.NewString()
	}

	return &SearchClient{
		baseURL:         baseURL,
		subscriptionKey: subscriptionKey,
		reese84:         strings.TrimSpace(cfg.Reese84),
		visitorID:       visitorID,
		httpClient:      httpClient,
	}, nil
}

func (c *SearchClient) Search(ctx context.Context, storeID, zipCode string, opts SearchOptions) (*SearchResponse, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	zipCode = strings.TrimSpace(zipCode)
	if zipCode == "" {
		return nil, errors.New("zip code is required")
	}

	endpoint, err := url.Parse(c.baseURL + defaultSearchPath)
	if err != nil {
		return nil, fmt.Errorf("parse search URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("url", c.baseURL)
	query.Set("q", strings.TrimSpace(opts.Query))
	query.Set("rows", fmt.Sprintf("%d", normalizedRows(opts.Rows)))
	query.Set("start", fmt.Sprintf("%d", normalizedStart(opts.Start)))
	query.Set("channel", defaultSearchChannel)
	query.Set("storeid", storeID)
	query.Set("sort", strings.TrimSpace(opts.Sort))
	query.Set("widget-id", defaultString(opts.WidgetID, defaultSearchWidget))
	endpoint.RawQuery = query.Encode()

	log.Printf("search endpoint: %s", endpoint.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("ocp-apim-subscription-key", c.subscriptionKey)

	req.AddCookie(&http.Cookie{Name: "reese84", Value: c.reese84})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("search request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return &SearchResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
	}, nil
}

func normalizedRows(rows int) int {
	if rows <= 0 {
		return defaultSearchRows
	}
	return rows
}

func normalizedStart(start int) int {
	if start < 0 {
		return 0
	}
	return start
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
