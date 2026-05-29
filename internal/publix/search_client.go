package publix

import (
	"bytes"
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

const DefaultSearchBaseURL = "https://services.publix.com"

const (
	storeProductsSavingsSource = "WEB_SEARCH"
	storeProductsSavingsSort   = "srchViewsMonth desc, srchViewsYear desc"
	// this seems sketchy. I guess the date is 2 years old but would ratehr get rid of it and the query arg above.
	storeProductsSavingsXSrc      = "WEB_SEARCH_20240506"
	storeProductsSavingsUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36"
)

type SearchClient struct {
	baseURL    string
	httpClient *http.Client
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
	ItemCode int    `json:"itemCode"`
	Title    string `json:"title"`
	//shortDescription?
	//titleBrand?
	PriceLine         *string `json:"priceLine"`
	OriginalPriceLine *string `json:"originalPriceLine"`
	SizeDescription   *string `json:"sizeDescription"`
}

func NewSearchClient(httpClient *http.Client) *SearchClient {
	return NewSearchClientWithBaseURL(DefaultSearchBaseURL, httpClient)
}

func NewSearchClientWithBaseURL(baseURL string, httpClient *http.Client) *SearchClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultSearchBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &SearchClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *SearchClient) StoreProductsSavings(ctx context.Context, opts StoreProductsSavingsOptions) (*StoreProductsSavingsResult, error) {
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

	endpoint, err := url.Parse(c.baseURL + "/search/api/search/storeproductssavings/")
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
		Variables: storeProductsSavingsVariables{
			Take:       opts.Take,
			Skip:       opts.Skip,
			Keyword:    "",
			CategoryID: opts.CategoryID,
			SortOrder:  storeProductsSavingsSort,
			Source:     storeProductsSavingsSource,
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
	req.Header.Set("Cookie", "_abck="+strings.TrimSpace(opts.Abck))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		if err != nil {
			body = []byte(fmt.Sprintf("failed to read response body: %v", err))
		}
		return nil, fmt.Errorf("request %q: unexpected status %d: %s", endpoint.String(), resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var graphQLResp storeProductsSavingsGraphQLResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&graphQLResp); err != nil {
		return nil, fmt.Errorf("decode publix savings response: %w", err)
	}
	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("publix savings graphql error: %s", graphQLResp.Errors[0].Message)
	}

	return &graphQLResp.Data.StoreProductsSavingsSearchResult, nil
}

type storeProductsSavingsGraphQLRequest struct {
	Variables storeProductsSavingsVariables `json:"variables"`
	Query     string                        `json:"query"`
}

type storeProductsSavingsVariables struct {
	Take       int    `json:"take"`
	Skip       int    `json:"skip"`
	Keyword    string `json:"keyword"`
	CategoryID string `json:"categoryID"`
	SortOrder  string `json:"sortOrder"`
	Source     string `json:"source"`
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

// Publix returns products without priceLine/originalPriceLine when we ask for
// only a compact product field set. Keep the broader product-card selection,
// even though StoreProduct decodes only a few fields.
const storeProductsSavingsQuery = `query ($keyword: String, $skip: Int, $take: Int, $categoryID: String, $sortOrder: String, $source: String) {
  storeProductsSavingsSearchResult(
    keyword: $keyword
    skip: $skip
    take: $take
    sortOrder: $sortOrder
    categoryID: $categoryID
    source: $source
  ) {
    storeProducts {
      baseProductId
      itemCode
      title
      shortDescription
      srchAttr_cardDescription
      sizeDescription
      savingLine
      onSale
      priceLine
      specialPromotionDescription
      isCatering
      isCateringAddon
      originalPriceLine
      promoConditionsMsg
      promoMsg
      promoType
      promoValidThruMsg
      promoTotalSavings
      onTpr
      inStoreLocation
      storeNbr
      hasCoupon
      titleBrand
      fauxTaxonomy
    }
    totalCount
  }
}`
