// Package mnfoodclub sources MNFood.Club ingredients from BigCommerce.
package mnfoodclub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

// AllCategories selects the full catalog without a category filter.
const AllCategories = 0

// Client reads the complete store catalog using a store-level access token.
// Required credentials are checked at construction.
type Client struct {
	config     config.MNFoodClubConfig
	httpClient *http.Client
}

func NewClient(cfg config.MNFoodClubConfig, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(cfg.APIPath) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("MNFoodClub requires MNFOODCLUB_API_PATH and MNFOODCLUB_ACCESS_TOKEN")
	}
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = httpClient
	retryClient.RequestLogHook = httpretry.LogRetry("mnfoodclub")
	retryClient.Backoff = catalogBackoff
	return &Client{config: cfg, httpClient: retryClient.StandardClient()}, nil
}

// catalogBackoff honors BigCommerce's X-Rate-Limit-Time-Reset-Ms (milliseconds)
// and X-Retry-After (seconds), using the longer wait when both are present.
// These differ from the standard Retry-After header handled by the library.
// https://docs.bigcommerce.com/developer/docs/admin/catalog-and-inventory/data-migration/catalog/error-handling
func catalogBackoff(min, max time.Duration, attempt int, resp *http.Response) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		reset, resetErr := time.ParseDuration(resp.Header.Get("X-Rate-Limit-Time-Reset-Ms") + "ms")
		retryAfter, retryAfterErr := time.ParseDuration(resp.Header.Get("X-Retry-After") + "s")
		if resetErr == nil && reset >= 0 {
			if retryAfterErr == nil && retryAfter > reset {
				return retryAfter
			}
			return reset
		}
		if retryAfterErr == nil && retryAfter >= 0 {
			return retryAfter
		}
	}
	return retryablehttp.DefaultBackoff(min, max, attempt, resp)
}

func (c *Client) catalogURL() (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(c.config.APIPath))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid MNFOODCLUB_API_PATH: expected an absolute store API URL")
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 5 || parts[1] != "stores" || parts[2] == "" || parts[3] != "v3" || parts[4] != "" {
		return nil, fmt.Errorf("invalid MNFOODCLUB_API_PATH: expected /stores/<store_hash>/v3/")
	}
	u.Path += "catalog/"
	return u, nil
}

type catalogPage[T any] struct {
	Data []T `json:"data"`
	Meta struct {
		Pagination *struct {
			CurrentPage int `json:"current_page"`
			TotalPages  int `json:"total_pages"`
		} `json:"pagination"`
	} `json:"meta"`
}

func catalogItems[T any](ctx context.Context, c *Client, base *url.URL, resource string, filters url.Values) ([]T, error) {
	var items []T
	for page := 1; ; page++ {
		u := *base
		u.Path += resource
		query := url.Values{"limit": {"250"}, "page": {strconv.Itoa(page)}}
		for key, values := range filters {
			query[key] = values
		}
		u.RawQuery = query.Encode()
		var result catalogPage[T]
		if err := c.getPage(ctx, u.String(), &result); err != nil {
			return nil, fmt.Errorf("BigCommerce %s page %d: %w", resource, page, err)
		}
		pagination := result.Meta.Pagination
		if pagination == nil || pagination.CurrentPage != page || (pagination.TotalPages < page && len(result.Data) != 0) {
			return nil, fmt.Errorf("BigCommerce %s page %d: invalid pagination", resource, page)
		}
		items = append(items, result.Data...)
		if page >= pagination.TotalPages {
			return items, nil
		}
	}
}

func (c *Client) getPage(ctx context.Context, pageURL string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return fmt.Errorf("create catalog request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.config.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch catalog: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode catalog response: %w", err)
	}
	return nil
}

// Fetch returns every eligible product, optionally restricted to one category.
// Use AllCategories to fetch products across all categories.
func (c *Client) Fetch(ctx context.Context, categoryID int) ([]ai.InputIngredient, error) {
	base, err := c.catalogURL()
	if err != nil {
		return nil, err
	}
	filters := url.Values{"is_visible": {"true"}, "include": {"variants"}, "sort": {"id"}, "direction": {"asc"}}
	if categoryID != AllCategories {
		filters.Set("categories:in", strconv.Itoa(categoryID))
	}
	products, err := catalogItems[catalogProduct](ctx, c, base, "products", filters)
	if err != nil {
		return nil, err
	}
	categories, err := catalogItems[catalogCategory](ctx, c, base, "categories", nil)
	if err != nil {
		return nil, err
	}
	brands, err := catalogItems[catalogBrand](ctx, c, base, "brands", nil)
	if err != nil {
		return nil, err
	}
	return mapCatalog(products, categories, brands)
}
