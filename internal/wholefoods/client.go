package wholefoods

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

const (
	// DefaultBaseURL is the public Whole Foods Market website origin.
	DefaultBaseURL = "https://www.wholefoodsmarket.com"
)

// Client calls the public Whole Foods category products endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Response matches the public category API payload shape used in wf-output/beef.json.
type Response struct {
	Facets     []Facet      `json:"facets"`
	Breadcrumb []Breadcrumb `json:"breadcrumb"`
	Results    []Product    `json:"results"`
	Meta       Meta         `json:"meta"`
}

type Facet struct {
	Label       string            `json:"label"`
	Slug        string            `json:"slug"`
	Type        string            `json:"type,omitempty"`
	Refinements []FacetRefinement `json:"refinements"`
}

type FacetRefinement struct {
	Label       string            `json:"label"`
	Slug        string            `json:"slug"`
	Count       int               `json:"count"`
	IsSelected  bool              `json:"isSelected"`
	Disabled    bool              `json:"disabled"`
	Refinements []FacetRefinement `json:"refinements,omitempty"`
}

type Breadcrumb struct {
	Label string `json:"label"`
	Slug  string `json:"slug"`
}

type Product struct {
	RegularPrice         float64 `json:"regularPrice"`
	SalePrice            float64 `json:"salePrice,omitempty"`
	IncrementalSalePrice float64 `json:"incrementalSalePrice,omitempty"`
	SaleStartDate        string  `json:"saleStartDate,omitempty"`
	SaleEndDate          string  `json:"saleEndDate,omitempty"`
	Name                 string  `json:"name"`
	Slug                 string  `json:"slug"`
	Brand                string  `json:"brand"`
	ImageThumbnail       string  `json:"imageThumbnail"`
	Store                int     `json:"store"`
	IsLocal              bool    `json:"isLocal"`
	UOM                  string  `json:"uom,omitempty"`
}

type Meta struct {
	Total Total `json:"total"`
	State State `json:"state"`
}

type Total struct {
	Value    int    `json:"value"`
	Relation string `json:"relation"`
}

type State struct {
	Refinements []StateRefinement `json:"refinements"`
	Sort        string            `json:"sort"`
}

type StateRefinement struct {
	Label      string `json:"label"`
	Slug       string `json:"slug"`
	FilterSlug string `json:"filterSlug"`
}

// NewClient creates a Whole Foods client with a default base URL and timeout.
func NewClient(httpClient *http.Client) *Client {
	return NewClientWithBaseURL(DefaultBaseURL, httpClient)
}

// NewClientWithBaseURL creates a Whole Foods client for the provided base URL.
func NewClientWithBaseURL(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// Category fetches a category page payload like /api/products/category/beef?store=10216.
func (c *Client) Category(ctx context.Context, queryterm, store string) (*Response, error) {
	queryterm = strings.TrimSpace(queryterm)
	if queryterm == "" {
		return nil, errors.New("queryterm is required")
	}

	store = strings.TrimSpace(store)
	if store == "" {
		return nil, errors.New("store is required")
	}

	endpoint, err := url.Parse(c.baseURL + "/api/products/category/" + url.PathEscape(queryterm))
	if err != nil {
		return nil, fmt.Errorf("parse category URL: %w", err)
	}

	params := endpoint.Query()
	params.Set("store", store)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build category request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request category %q: %w", queryterm, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read category response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("category request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded Response
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode category response: %w", err)
	}

	return &decoded, nil
}
