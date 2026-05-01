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
)

// this is a strange set. Actual sub categories don't work but thes aisle-vs ones do.
// for en example broke sub category here is beef https://www.safeway.com/shop/aisles/meat-seafood/beef.html?sort=&page=1&loc=1142
const (
	Category_Vegatables   = "GR-C-categ-8c62c848"
	Category_Fruit        = "GR-C-categ-a8eea474"
	Category_Seafood      = "GR-C-Categ-6090cd27" //  https://www.safeway.com/aisle-vs/meat-seafood/seafood-favorites.html
	Category_Meat         = "GR-MeatF-fffc8662"   // https://www.safeway.com/aisle-vs/meat-seafood/meat-favorites.html
	Category_Wine         = "GR-S-Searc-db592d50"
	Category_Pasta_Grains = "GR-C-Categ-77b9d5dd" // https://www.safeway.com/aisle-vs/grains-pasta-sides/best-sellers.html
	Category_Dairy        = "GR-C-Categ-f210e5cd" // new and trending seems dubious https://www.safeway.com/aisle-vs/dairy-eggs-cheese/new-trending.html
)

func StapleCategories() []string {
	return []string{
		Category_Vegatables,
		Category_Fruit,
		Category_Seafood,
		Category_Meat,
		Category_Pasta_Grains,
	}
}

const (
	DefaultSearchBaseURL = "https://www.safeway.com"
	defaultSearchPath    = "/abs/pub/xapi/wcax/pathway/search"
	defaultSearchRows    = 60 // how high can we go. Shoudl we paginate just to
	defaultSearchChannel = "instore"
	defaultSearchUser    = "G"
)

type SearchClient struct {
	baseURL         string
	subscriptionKey string
	reese84Provider func(context.Context) (string, error) // interface?
	httpClient      *http.Client
}

type SearchClientConfig struct {
	BaseURL         string
	SubscriptionKey string
	Reese84Provider func(context.Context) (string, error)
	HTTPClient      *http.Client
}

type SearchOptions struct {
	Query string
	Start uint
	Rows  uint
	Sort  string
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
		httpClient = http.DefaultClient
	}

	return &SearchClient{
		baseURL:         baseURL,
		subscriptionKey: subscriptionKey,
		reese84Provider: cfg.Reese84Provider,
		httpClient:      httpClient,
	}, nil
}

func (c *SearchClient) Search(ctx context.Context, storeID, category string, opts SearchOptions) (*PathwaySearchPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*20)
	defer cancel()

	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	endpoint, err := url.Parse(c.baseURL + defaultSearchPath)
	if err != nil {
		return nil, fmt.Errorf("parse search URL: %w", err)
	}
	if opts.Rows == 0 {
		opts.Rows = defaultSearchRows
	}

	query := endpoint.Query()
	query.Set("url", c.baseURL)
	query.Set("q", strings.TrimSpace(opts.Query))
	query.Set("rows", fmt.Sprintf("%d", opts.Rows))
	query.Set("start", fmt.Sprintf("%d", opts.Start))
	query.Set("channel", defaultSearchChannel)
	query.Set("storeid", storeID)
	query.Set("sort", strings.TrimSpace(opts.Sort))
	query.Set("widget-id", category)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("ocp-apim-subscription-key", c.subscriptionKey)

	reese84, err := c.reese84Provider(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve reese84: %w", err)
	}

	req.AddCookie(&http.Cookie{Name: "reese84", Value: strings.TrimSpace(reese84)})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errbody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(errbody)))
	}

	var payload PathwaySearchPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode json response: %w", err)
	}
	return &payload, nil
}
