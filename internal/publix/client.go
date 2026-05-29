package publix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

const DefaultBaseURL = "https://www.publix.com"
const DefaultSearchBaseURL = "https://services.publix.com"

const (
	storeProductsSavingsOperationName = "GetStoreProductsSavingsSearchResultAsync"
	storeProductsSavingsSource        = "WEB_SEARCH"
	storeProductsSavingsXSrc          = "WEB_SEARCH_20240506"
	storeProductsSavingsUserAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
)

type Client struct {
	baseURL       string
	searchBaseURL string
	httpClient    *http.Client
}

type ProbeResult struct {
	StoreID string
	Exists  bool
	URL     string
}

type StoreSummary struct {
	ID      string   `json:"id"`
	StoreID string   `json:"store_id"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	City    string   `json:"city"`
	State   string   `json:"state"`
	ZipCode string   `json:"zip_code"`
	URL     string   `json:"url"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`
}

type StoreProductsSavingsOptions struct {
	StoreNumber string
	CategoryID  string
	Abck        string
	Take        int
	Skip        int
}

type StoreProductsSavingsResult struct {
	StoreProducts []StoreProduct `json:"storeProducts"`
	TotalCount    int            `json:"totalCount"`
}

type StoreProduct struct {
	ItemCode        int     `json:"itemCode"`
	Title           string  `json:"title"`
	PriceLine       *string `json:"priceLine"`
	SizeDescription *string `json:"sizeDescription"`
}

type storePage struct {
	StoreNumber int          `json:"storeNumber"`
	Name        string       `json:"name"`
	Address     storeAddress `json:"address"`
	Latitude    float64      `json:"latitude"`
	Longitude   float64      `json:"longitude"`
}

type storeAddress struct {
	StreetAddress string `json:"streetAddress"`
	City          string `json:"city"`
	State         string `json:"state"`
	Zip           string `json:"zip"`
}

func NewClient(httpClient *http.Client) *Client {
	return NewClientWithBaseURLs(DefaultBaseURL, DefaultSearchBaseURL, httpClient)
}

func NewClientWithBaseURL(baseURL string, httpClient *http.Client) *Client {
	return NewClientWithBaseURLs(baseURL, baseURL, httpClient)
}

func NewClientWithBaseURLs(baseURL, searchBaseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	searchBaseURL = strings.TrimSpace(searchBaseURL)
	if searchBaseURL == "" {
		searchBaseURL = DefaultSearchBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	cloned := *httpClient
	cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		searchBaseURL: strings.TrimRight(searchBaseURL, "/"),
		httpClient:    &cloned,
	}
}

func (c *Client) ResolveStore(ctx context.Context, storeID string) (*ProbeResult, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}
	if _, err := strconv.Atoi(storeID); err != nil {
		return nil, fmt.Errorf("store id %q must be numeric: %w", storeID, err)
	}

	endpoint := c.baseURL + "/locations/" + url.PathEscape(storeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build store probe request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		// why do we need to copy to discard here?
		// Because some servers (including Cloudflare) will not close the connection
		// if the response body is not fully read, which can lead to resource leaks and
		// exhaustion of available connections in the HTTP client's connection pool.
		// By copying the remaining data to io.Discard, we ensure that the entire
		// response body is read and the connection can be properly reused or closed by the server.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest:
		location := strings.TrimSpace(resp.Header.Get("Location"))
		if location == "" {
			return nil, fmt.Errorf("redirect missing location header for store %s", storeID)
		}
		resolved, err := req.URL.Parse(location)
		if err != nil {
			return nil, fmt.Errorf("parse redirect location %q: %w", location, err)
		}
		if isMissingStoreRedirect(resolved) {
			return &ProbeResult{StoreID: storeID, Exists: false}, nil
		}
		if strings.HasPrefix(resolved.Path, "/locations/") {
			return &ProbeResult{StoreID: storeID, Exists: true, URL: resolved.String()}, nil
		}
		return nil, fmt.Errorf("unexpected redirect target %q for store %s", resolved.String(), storeID)
	case resp.StatusCode == http.StatusOK:
		return &ProbeResult{StoreID: storeID, Exists: true, URL: endpoint}, nil
	case resp.StatusCode == http.StatusNotFound:
		return &ProbeResult{StoreID: storeID, Exists: false}, nil
	default:
		return nil, fmt.Errorf("request %q: unexpected status %d", endpoint, resp.StatusCode)
	}
}

func (c *Client) StoreSummary(ctx context.Context, pageURL string) (*StoreSummary, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return nil, errors.New("page url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build store page request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", pageURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return ExtractStoreSummary(pageURL, body)
}

func ExtractStoreSummary(pageURL string, body []byte) (*StoreSummary, error) {
	payload, err := extractStorePayload(body)
	if err != nil {
		return nil, err
	}

	if payload.StoreNumber == 0 {
		return nil, errors.New("store number missing from publix store payload")
	}
	if strings.TrimSpace(payload.Name) == "" {
		return nil, errors.New("store name missing from publix store payload")
	}

	lat := payload.Latitude
	lon := payload.Longitude
	storeID := strconv.Itoa(payload.StoreNumber)

	return &StoreSummary{
		ID:      LocationIDPrefix + storeID,
		StoreID: storeID,
		Name:    strings.TrimSpace(payload.Name),
		Address: strings.TrimSpace(payload.Address.StreetAddress),
		City:    strings.TrimSpace(payload.Address.City),
		State:   strings.TrimSpace(payload.Address.State),
		ZipCode: normalizeZIP(payload.Address.Zip),
		URL:     strings.TrimSpace(pageURL),
		Lat:     &lat,
		Lon:     &lon,
	}, nil
}

func extractStorePayload(body []byte) (*storePage, error) {
	tokenizer := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if err := tokenizer.Err(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil, errors.New("publix store payload not found")
				}
				return nil, fmt.Errorf("tokenize publix store page: %w", err)
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			for _, attr := range token.Attr {
				if attr.Key != ":store" {
					continue
				}

				raw := html.UnescapeString(attr.Val)
				var payload storePage
				if err := json.Unmarshal([]byte(raw), &payload); err != nil {
					return nil, fmt.Errorf("decode publix store payload: %w", err)
				}
				return &payload, nil
			}
		}
	}
}

func normalizeZIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if dash := strings.IndexByte(raw, '-'); dash >= 0 {
		raw = raw[:dash]
	}
	if len(raw) > 5 {
		raw = raw[:5]
	}
	return raw
}

func isMissingStoreRedirect(u *url.URL) bool {
	path := strings.TrimRight(strings.TrimSpace(u.Path), "/")
	return path == "/locations"
}

func (c *Client) StoreProductsSavings(ctx context.Context, opts StoreProductsSavingsOptions) (*StoreProductsSavingsResult, error) {
	opts.StoreNumber = strings.TrimSpace(opts.StoreNumber)
	opts.CategoryID = strings.TrimSpace(opts.CategoryID)
	opts.Abck = strings.TrimSpace(opts.Abck)

	if opts.StoreNumber == "" {
		return nil, errors.New("store number is required")
	}
	if opts.CategoryID == "" {
		return nil, errors.New("category id is required")
	}
	if opts.Abck == "" {
		return nil, errors.New("abck token is required")
	}
	if opts.Take <= 0 {
		return nil, errors.New("take must be positive")
	}
	if opts.Skip < 0 {
		return nil, errors.New("skip must be non-negative")
	}

	endpoint, err := url.Parse(c.searchBaseURL + "/search/api/search/storeproductssavings/")
	if err != nil {
		return nil, fmt.Errorf("parse publix savings URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("keyword", "")
	query.Set("storeNumber", opts.StoreNumber)
	query.Set("cat", opts.CategoryID)
	query.Set("source", storeProductsSavingsSource)
	endpoint.RawQuery = query.Encode()

	payload := storeProductsSavingsGraphQLRequest{
		OperationName: storeProductsSavingsOperationName,
		Variables: storeProductsSavingsVariables{
			Take:             opts.Take,
			Skip:             opts.Skip,
			SortOrder:        "srchViewsMonth desc, srchViewsYear desc",
			IsPU:             false,
			CategoryID:       opts.CategoryID,
			Keyword:          "",
			Facets:           "",
			MinMatch:         -41,
			BoostVarIndex:    1,
			WildcardSearch:   false,
			IsPreviewSite:    false,
			GetOrderHistory:  false,
			FilterQuery:      "",
			ReorderItemCodes: nil,
			BoostBuryQuery:   "",
			ElevatedProducts: []storeProductsSavingsKeyValue{},
			ForceElevation:   false,
			SearchRetryIndex: 0,
			Source:           storeProductsSavingsSource,
			SearchVariation:  []storeProductsSavingsKeyValue{{Key: "configurable_add_to_cart", Value: "true"}, {Key: "boost_field", Value: "A"}},
			SegmentVarIndex:  1,
			Intents:          []string{},
			IntentVarIndex:   1,
		},
		Query: storeProductsSavingsQuery,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal publix savings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(rawPayload))
	if err != nil {
		return nil, fmt.Errorf("build publix savings request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", DefaultBaseURL)
	req.Header.Set("Referer", DefaultBaseURL+"/")
	req.Header.Set("PublixStore", opts.StoreNumber)
	req.Header.Set("User-Agent", storeProductsSavingsUserAgent)
	req.Header.Set("X-Src", storeProductsSavingsXSrc)
	req.Header.Set("Cookie", abckCookie(opts.Abck))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read publix savings response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request %q: unexpected status %d: %s", endpoint.String(), resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var graphQLResp storeProductsSavingsGraphQLResponse
	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		return nil, fmt.Errorf("decode publix savings response: %w", err)
	}
	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("publix savings graphql error: %s", graphQLResp.Errors[0].Message)
	}

	return &graphQLResp.Data.StoreProductsSavingsSearchResult, nil
}

type storeProductsSavingsGraphQLRequest struct {
	OperationName string                        `json:"operationName"`
	Variables     storeProductsSavingsVariables `json:"variables"`
	Query         string                        `json:"query"`
}

type storeProductsSavingsVariables struct {
	Take             int                            `json:"take"`
	Skip             int                            `json:"skip"`
	SortOrder        string                         `json:"sortOrder"`
	IsPU             bool                           `json:"ispu"`
	CategoryID       string                         `json:"categoryID"`
	Keyword          string                         `json:"keyword"`
	FacetOverrideStr *string                        `json:"facetOverrideStr"`
	Facets           string                         `json:"facets"`
	MinMatch         int                            `json:"minMatch"`
	BoostVarIndex    int                            `json:"boostVarIndex"`
	WildcardSearch   bool                           `json:"wildcardSearch"`
	IsPreviewSite    bool                           `json:"isPreviewSite"`
	GetOrderHistory  bool                           `json:"getOrderHistory"`
	FilterQuery      string                         `json:"filterQuery"`
	ReorderItemCodes []int                          `json:"reorderItemCodes"`
	BoostBuryQuery   string                         `json:"boostBuryQuery"`
	ElevatedProducts []storeProductsSavingsKeyValue `json:"elevatedProducts"`
	ForceElevation   bool                           `json:"forceElevation"`
	SearchRetryIndex int                            `json:"searchRetryIndex"`
	Source           string                         `json:"source"`
	SearchVariation  []storeProductsSavingsKeyValue `json:"searchVariation"`
	SegmentVarIndex  int                            `json:"segmentVarIndex"`
	Intents          []string                       `json:"intents"`
	UserCoupon       *string                        `json:"userCoupon"`
	IntentVarIndex   int                            `json:"intentVarIndex"`
	CouponID         *string                        `json:"couponId"`
}

type storeProductsSavingsKeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type storeProductsSavingsGraphQLResponse struct {
	Data struct {
		StoreProductsSavingsSearchResult StoreProductsSavingsResult `json:"storeProductsSavingsSearchResult"`
	} `json:"data"`
	Errors []graphQLError `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

const storeProductsSavingsQuery = `query GetStoreProductsSavingsSearchResultAsync($keyword: String, $skip: Int!, $take: Int!, $facetOverrideStr: String, $facets: String, $sortOrder: String, $ispu: Boolean, $categoryID: String, $minMatch: Int!, $boostVarIndex: Int!, $wildcardSearch: Boolean!, $isPreviewSite: Boolean!, $segmentVarIndex: Int!, $getOrderHistory: Boolean!, $filterQuery: String, $reorderItemCodes: [Int!], $intents: [String!], $searchRetryIndex: Int!, $intentVarIndex: Int!, $boostBuryQuery: String, $source: String, $elevatedProducts: [KeyValuePairOfStringAndStringInput!], $couponId: String, $forceElevation: Boolean, $searchVariation: [KeyValuePairOfStringAndStringInput!], $userCoupon: String) {
  storeProductsSavingsSearchResult(
    keyword: $keyword
    skip: $skip
    take: $take
    facetOverrideStr: $facetOverrideStr
    facets: $facets
    sortOrder: $sortOrder
    ispu: $ispu
    categoryID: $categoryID
    minMatch: $minMatch
    boostVarIndex: $boostVarIndex
    wildcardSearch: $wildcardSearch
    isPreviewSite: $isPreviewSite
    segmentVarIndex: $segmentVarIndex
    getOrderHistory: $getOrderHistory
    filterQuery: $filterQuery
    reorderItemCodes: $reorderItemCodes
    intents: $intents
    boostBuryQuery: $boostBuryQuery
    searchRetryIndex: $searchRetryIndex
    intentVarIndex: $intentVarIndex
    source: $source
    elevatedProducts: $elevatedProducts
    couponId: $couponId
    forceElevation: $forceElevation
    searchVariation: $searchVariation
    userCoupon: $userCoupon
  ) {
    storeProducts {
      itemCode
      title
      sizeDescription
      priceLine
    }
    totalCount
  }
}
`

func abckCookie(abck string) string {
	abck = strings.TrimSpace(abck)
	if strings.Contains(abck, "_abck=") {
		return abck
	}
	return "_abck=" + abck
}
