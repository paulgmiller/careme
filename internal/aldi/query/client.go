package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultBaseURL = "https://www.aldi.us"

	operationName      = "CollectionProductsWithFeaturedProducts"
	persistedQueryHash = "f3193dacfeec83828016dc1b3c8af8e61c4470d3f466da2d5797b3f2c530369c"

	defaultFirst            = 4
	defaultOrderBy          = "bestMatch"
	defaultItemsDisplayType = "collections_nav_child_carousel"
	defaultPageSource       = "browse"
)

type Client struct {
	baseURL        string
	httpClient     *http.Client
	forterToken    string
	pageViewIDFunc func() string
}

type ClientConfig struct {
	BaseURL        string
	HTTPClient     *http.Client
	ForterToken    string
	PageViewIDFunc func() string
}

type SearchOptions struct {
	PostalCode string
	ZoneID     string
	First      int
	OrderBy    string
	PageViewID string
}

type collectionProductsVariables struct {
	ShopID           string   `json:"shopId"`
	PostalCode       string   `json:"postalCode,omitempty"`
	ZoneID           string   `json:"zoneId,omitempty"`
	Slug             string   `json:"slug"`
	OrderBy          string   `json:"orderBy"`
	Filters          []string `json:"filters"`
	PageViewID       string   `json:"pageViewId"`
	ItemsDisplayType string   `json:"itemsDisplayType"`
	First            int      `json:"first"`
	PageSource       string   `json:"pageSource"`
}

type persistedQueryExtensions struct {
	PersistedQuery persistedQuery `json:"persistedQuery"`
}

type persistedQuery struct {
	Version    int    `json:"version"`
	SHA256Hash string `json:"sha256Hash"`
}

func NewClient(cfg ClientConfig) *Client {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	pageViewIDFunc := cfg.PageViewIDFunc
	if pageViewIDFunc == nil {
		pageViewIDFunc = uuid.NewString
	}

	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     httpClient,
		forterToken:    strings.TrimSpace(cfg.ForterToken),
		pageViewIDFunc: pageViewIDFunc,
	}
}

func (c *Client) CollectionProducts(ctx context.Context, storeID, categorySlug string, opts SearchOptions) (*CollectionProductsPayload, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	categorySlug = strings.TrimSpace(categorySlug)
	if categorySlug == "" {
		return nil, errors.New("category slug is required")
	}

	pageViewID := strings.TrimSpace(opts.PageViewID)
	if pageViewID == "" {
		pageViewID = strings.TrimSpace(c.pageViewIDFunc())
	}
	if pageViewID == "" {
		return nil, errors.New("page view id is required")
	}
	if opts.First < 0 {
		return nil, errors.New("first must be greater than or equal to 0")
	}

	endpoint, err := c.collectionProductsURL(storeID, categorySlug, pageViewID, opts)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Identifier", "web")
	req.Header.Set("X-Page-View-Id", pageViewID)
	if c.forterToken != "" {
		req.AddCookie(&http.Cookie{Name: "forterToken", Value: c.forterToken})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("collection products request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload CollectionProductsPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode collection products response: %w", err)
	}
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("collection products GraphQL error: %s", graphQLErrorsString(payload.Errors))
	}
	return &payload, nil
}

func (c *Client) collectionProductsURL(storeID, categorySlug, pageViewID string, opts SearchOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/graphql")
	if err != nil {
		return "", fmt.Errorf("parse collection products URL: %w", err)
	}

	first := opts.First
	if first == 0 {
		first = defaultFirst
	}
	orderBy := strings.TrimSpace(opts.OrderBy)
	if orderBy == "" {
		orderBy = defaultOrderBy
	}

	variables := collectionProductsVariables{
		ShopID:           storeID,
		PostalCode:       strings.TrimSpace(opts.PostalCode),
		ZoneID:           strings.TrimSpace(opts.ZoneID),
		Slug:             categorySlug,
		OrderBy:          orderBy,
		Filters:          []string{},
		PageViewID:       pageViewID,
		ItemsDisplayType: defaultItemsDisplayType,
		First:            first,
		PageSource:       defaultPageSource,
	}
	rawVariables, err := json.Marshal(variables)
	if err != nil {
		return "", fmt.Errorf("marshal variables: %w", err)
	}

	extensions := persistedQueryExtensions{
		PersistedQuery: persistedQuery{
			Version:    1,
			SHA256Hash: persistedQueryHash,
		},
	}
	rawExtensions, err := json.Marshal(extensions)
	if err != nil {
		return "", fmt.Errorf("marshal extensions: %w", err)
	}

	query := endpoint.Query()
	query.Set("operationName", operationName)
	query.Set("variables", string(rawVariables))
	query.Set("extensions", string(rawExtensions))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func graphQLErrorsString(errs []GraphQLError) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		message := strings.TrimSpace(err.Message)
		if message == "" {
			message = "unknown error"
		}
		parts = append(parts, message)
	}
	return strings.Join(parts, "; ")
}
