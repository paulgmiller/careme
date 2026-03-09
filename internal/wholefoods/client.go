package wholefoods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// CategoryResponse matches the public category API payload shape used in wf-output/beef.json.
type CategoryResponse struct {
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

// StoreSummaryResponse matches the public store summary payload returned by /api/stores/{store}/summary.
type StoreSummaryResponse struct {
	StoreID                  int               `json:"storeId"`
	Token                    string            `json:"token"`
	DisplayName              string            `json:"displayName"`
	Status                   string            `json:"status"`
	Phone                    string            `json:"phone"`
	StorePrimeEligibility    bool              `json:"storePrimeEligibility"`
	StoreOperationalGuidance string            `json:"storeOperationalGuidance"`
	BU                       int               `json:"bu"`
	Folder                   string            `json:"folder"`
	OpenedAt                 string            `json:"openedAt"`
	Links                    StoreSummaryLinks `json:"links"`
	PrimaryLocation          StoreLocation     `json:"primaryLocation"`
	Hours                    map[string]string `json:"hours"`
	Holidays                 map[string]any    `json:"holidays"`
}

type StoreSummaryLinks struct {
	Details                   string `json:"Details"`
	Directions                string `json:"Directions"`
	Sales                     string `json:"Sales"`
	PrimeNowPickUpAndDelivery string `json:"PrimeNowPickUpAndDelivery"`
	MapURLDesktop             string `json:"MapUrlDesktop"`
	MapURLTablet              string `json:"MapUrlTablet"`
	MapURLMobile              string `json:"MapUrlMobile"`
}

type StoreLocation struct {
	Address   StoreAddress `json:"address"`
	Latitude  float64      `json:"latitude"`
	Longitude float64      `json:"longitude"`
}

type StoreAddress struct {
	StreetAddressLine1 string `json:"STREET_ADDRESS_LINE1"`
	City               string `json:"CITY"`
	State              string `json:"STATE"`
	PostalCode         string `json:"POSTAL_CODE"`
	ZipCode            string `json:"ZIP_CODE"`
	Country            string `json:"COUNTRY"`
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
func (c *Client) Category(ctx context.Context, queryterm, store string) (*CategoryResponse, error) {
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
	slog.InfoContext(ctx, "wf category search", "url", endpoint)
	var decoded CategoryResponse
	if err := c.getJSON(ctx, endpoint.String(), &decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}

// StoreSummary fetches a store summary payload like /api/stores/10216/summary.
func (c *Client) StoreSummary(ctx context.Context, store string) (*StoreSummaryResponse, error) {
	store = strings.TrimSpace(store)
	if store == "" {
		return nil, errors.New("store is required")
	}

	endpoint := c.baseURL + "/api/stores/" + url.PathEscape(store) + "/summary"

	var decoded StoreSummaryResponse
	if err := c.getJSON(ctx, endpoint, &decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
