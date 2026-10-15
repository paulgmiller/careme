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
)

// Client reads the complete store catalog using a store-level access token.
// Configuration is checked on use so missing credentials do not disable other stores.
type Client struct {
	config     config.MNFoodClubConfig
	httpClient *http.Client
}

func NewClient(cfg config.MNFoodClubConfig, httpClient *http.Client) *Client {
	return &Client{config: cfg, httpClient: httpClient}
}

func (c *Client) catalogURL() (*url.URL, error) {
	if strings.TrimSpace(c.config.APIPath) == "" || strings.TrimSpace(c.config.AccessToken) == "" {
		return nil, fmt.Errorf("MNFoodClub requires BIGCOMMERCE_API_PATH and BIGCOMMERCE_ACCESS_TOKEN")
	}
	u, err := url.Parse(strings.TrimSpace(c.config.APIPath))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid BIGCOMMERCE_API_PATH: expected an absolute store API URL")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/v2"), "/v3")
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "stores" || parts[1] == "" {
		return nil, fmt.Errorf("invalid BIGCOMMERCE_API_PATH: expected /stores/<store_hash>/v3")
	}
	u.Path = path + "/v3/catalog/"
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
	for attempt := 0; ; attempt++ {
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
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			delay := time.Second
			if ms, err := strconv.ParseInt(resp.Header.Get("X-Rate-Limit-Time-Reset-Ms"), 10, 64); err == nil && ms >= 0 {
				// Parse as milliseconds without overflowing a time.Duration.
				if ms > int64((1<<63-1)/time.Millisecond) {
					_ = resp.Body.Close()
					return fmt.Errorf("invalid BigCommerce rate limit reset interval")
				}
				delay = time.Duration(ms) * time.Millisecond
			}
			_ = resp.Body.Close()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(result)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("decode catalog response: %w", err)
		}
		return nil
	}
}

// Fetch returns every eligible product, optionally restricted to one category.
func (c *Client) Fetch(ctx context.Context, categoryID int) ([]ai.InputIngredient, error) {
	base, err := c.catalogURL()
	if err != nil {
		return nil, err
	}
	filters := url.Values{"is_visible": {"true"}, "include": {"variants"}, "sort": {"id"}, "direction": {"asc"}}
	if categoryID != 0 {
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
