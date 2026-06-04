package heb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"careme/internal/cache"
)

const (
	StoreLocationsSearchCachePrefix = "heb/store_location_search/"
	NextDataBuildIDCacheKey         = "heb/next_data_build_id.txt"

	defaultStoreLocationsTimeout  = 20 * time.Second
	defaultStoreLocationsMaxBytes = 16 * 1024 * 1024
)

type StoreLocationsClient struct {
	baseURL         string
	httpClient      *http.Client
	reese84Provider func(context.Context) (string, error)
	userAgent       string
}

type StoreLocationsClientConfig struct {
	BaseURL         string
	HTTPClient      *http.Client
	Reese84Provider func(context.Context) (string, error)
	UserAgent       string
}

type StoreLocationsPage struct {
	Summaries        []StoreSummary
	TotalStoresCount int
	CurrentPage      int
}

type storeLocationsPayload struct {
	PageProps storeLocationsPageProps `json:"pageProps"`
}

type storeLocationsPageProps struct {
	CurrentPageStores []storeLocationResult `json:"currentPageStores"`
	TotalStoresCount  int                   `json:"totalStoresCount"`
	CurrentPage       int                   `json:"currentPage"`
	SearchError       bool                  `json:"searchError"`
}

type storeLocationResult struct {
	Store storeLocationStore `json:"store"`
}

type storeLocationStore struct {
	Longitude   *float64             `json:"longitude"`
	Latitude    *float64             `json:"latitude"`
	StoreNumber int                  `json:"storeNumber"`
	Name        string               `json:"name"`
	Address     storeLocationAddress `json:"address"`
}

type storeLocationAddress struct {
	StreetAddress string `json:"streetAddress"`
	Locality      string `json:"locality"`
	Region        string `json:"region"`
	PostalCode    string `json:"postalCode"`
}

func NewStoreLocationsClient(cfg StoreLocationsClientConfig) *StoreLocationsClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultStoreLocationsTimeout}
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &StoreLocationsClient{
		baseURL:         baseURL,
		httpClient:      httpClient,
		reese84Provider: cfg.Reese84Provider,
		userAgent:       userAgent,
	}
}

func (c *StoreLocationsClient) StoreLocationsPage(ctx context.Context, buildID, address string, page int) ([]byte, error) {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return nil, fmt.Errorf("heb next data build id is required")
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("store location address is required")
	}
	if page <= 0 {
		page = 1
	}

	endpoint, err := c.storeLocationsDataURL(buildID, address, page)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build store locations request: %w", err)
	}
	if err := c.setStoreLocationsHeaders(ctx, req); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("store locations request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultStoreLocationsMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("read store locations response: %w", err)
	}
	if err := validateJSONBody(body, endpoint); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *StoreLocationsClient) storeLocationsDataURL(buildID, address string, page int) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/_next/data/" + url.PathEscape(buildID) + "/en/store-locations.json")
	if err != nil {
		return "", fmt.Errorf("parse store locations data URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("address", address)
	query.Set("page", strconv.Itoa(page))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *StoreLocationsClient) setStoreLocationsHeaders(ctx context.Context, req *http.Request) error {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Referer", c.baseURL+"/store-locations")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Linux"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Nextjs-Data", "1")
	if c.reese84Provider != nil {
		reese84, err := c.reese84Provider(ctx)
		if err != nil {
			return fmt.Errorf("load cached albertsons reese84 token for HEB store locations: %w", err)
		}
		reese84 = strings.TrimSpace(reese84)
		if reese84 != "" {
			req.Header.Set("Cookie", "reese84="+reese84)
		}
	}
	return nil
}

func DecodeStoreLocationsPage(body []byte) (*StoreLocationsPage, error) {
	if err := validateJSONBody(body, "store locations payload"); err != nil {
		return nil, err
	}

	var payload storeLocationsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, invalidStoreLocationsJSONError{
			source:  "store locations payload",
			snippet: bodySnippet(body),
			cause:   err,
		}
	}

	if payload.PageProps.SearchError {
		return nil, fmt.Errorf("store locations search returned an error")
	}

	summaries := make([]StoreSummary, 0, len(payload.PageProps.CurrentPageStores))
	for _, result := range payload.PageProps.CurrentPageStores {
		summary, ok := summaryFromStoreLocation(result.Store)
		if !ok {
			continue
		}
		summaries = append(summaries, summary)
	}

	return &StoreLocationsPage{
		Summaries:        summaries,
		TotalStoresCount: payload.PageProps.TotalStoresCount,
		CurrentPage:      payload.PageProps.CurrentPage,
	}, nil
}

type invalidStoreLocationsJSONError struct {
	source  string
	snippet string
	cause   error
}

func (e invalidStoreLocationsJSONError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("decode store locations json response from %s: %v: %q", e.source, e.cause, e.snippet)
	}
	return fmt.Sprintf("store locations response from %s is not JSON: %q", e.source, e.snippet)
}

func (e invalidStoreLocationsJSONError) Unwrap() error {
	return e.cause
}

func validateJSONBody(body []byte, source string) error {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return invalidStoreLocationsJSONError{source: source, snippet: ""}
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return invalidStoreLocationsJSONError{source: source, snippet: bodySnippet(body)}
	}
	return nil
}

func bodySnippet(body []byte) string {
	snippet := strings.TrimSpace(string(body))
	snippet = normalizeWhitespace(snippet)
	const maxSnippetLength = 240
	if len(snippet) > maxSnippetLength {
		snippet = snippet[:maxSnippetLength]
	}
	return snippet
}

func summaryFromStoreLocation(store storeLocationStore) (StoreSummary, bool) {
	if store.StoreNumber <= 0 {
		return StoreSummary{}, false
	}

	storeID := strconv.Itoa(store.StoreNumber)
	address := store.Address
	summary := StoreSummary{
		ID:      LocationIDPrefix + storeID,
		StoreID: storeID,
		Name:    normalizeWhitespace(store.Name),
		Address: normalizeWhitespace(address.StreetAddress),
		City:    normalizeWhitespace(address.Locality),
		State:   strings.ToUpper(strings.TrimSpace(address.Region)),
		ZipCode: normalizePostalCode(address.PostalCode),
		URL:     DefaultBaseURL + "/store-locations?storeNumber=" + url.QueryEscape(storeID),
		Lat:     store.Latitude,
		Lon:     store.Longitude,
	}

	if summary.Name == "" || summary.Address == "" || summary.State == "" || summary.ZipCode == "" {
		return StoreSummary{}, false
	}
	return summary, true
}

func normalizePostalCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 5 && isDigits(value[:5]) {
		return value[:5]
	}
	return value
}

func StoreLocationsPageCacheKey(address string, page int) string {
	if page <= 0 {
		page = 1
	}
	address = strings.TrimSpace(address)
	return StoreLocationsSearchCachePrefix + url.QueryEscape(address) + "/page-" + strconv.Itoa(page) + ".json"
}

func CacheStoreLocationsPage(ctx context.Context, c cache.Cache, address string, page int, body []byte) error {
	if len(body) == 0 {
		return fmt.Errorf("store locations response body is required")
	}
	if err := c.Put(ctx, StoreLocationsPageCacheKey(address, page), string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store locations cache: %w", err)
	}
	return nil
}

func LoadCachedStoreLocationsPage(ctx context.Context, c cache.Cache, address string, page int) (*StoreLocationsPage, error) {
	reader, err := c.Get(ctx, StoreLocationsPageCacheKey(address, page))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(reader, defaultStoreLocationsMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("read cached store locations response: %w", err)
	}
	return DecodeStoreLocationsPage(body)
}

func SaveNextDataBuildID(ctx context.Context, c cache.Cache, buildID string) error {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return fmt.Errorf("heb next data build id is required")
	}
	if err := c.Put(ctx, NextDataBuildIDCacheKey, buildID, cache.Unconditional()); err != nil {
		return fmt.Errorf("write heb next data build id cache: %w", err)
	}
	return nil
}

func LoadNextDataBuildID(ctx context.Context, c cache.Cache) (string, error) {
	reader, err := c.Get(ctx, NextDataBuildIDCacheKey)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = reader.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(reader, 1024))
	if err != nil {
		return "", fmt.Errorf("read heb next data build id cache: %w", err)
	}
	buildID := strings.TrimSpace(string(body))
	if buildID == "" {
		return "", fmt.Errorf("cached heb next data build id is empty")
	}
	return buildID, nil
}
