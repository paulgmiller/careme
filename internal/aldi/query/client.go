package query

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultBaseURL = "https://www.aldi.us"

	collectionProductsOperationName      = "CollectionProductsWithFeaturedProducts"
	collectionProductsPersistedQueryHash = "f3193dacfeec83828016dc1b3c8af8e61c4470d3f466da2d5797b3f2c530369c"
	itemsOperationName                   = "Items"
	itemsPersistedQueryHash              = "b2411f6acba21b2a6a277ca5616fdc5d1265ba647808895c05fe7ce1fd2fdcec"

	defaultFirst            = 4
	defaultOrderBy          = "bestMatch"
	defaultItemsDisplayType = "collections_all_items_grid"
	defaultPageSource       = "browse"
)

type Client struct {
	baseURL            string
	httpClient         *http.Client
	instacartSID       string
	instacartSessionID string
	cookieHeader       string
	debugWriter        io.Writer
	pageViewIDFunc     func() string
}

type ClientConfig struct {
	BaseURL            string
	HTTPClient         *http.Client
	InstacartSID       string
	InstacartSessionID string
	CookieHeader       string
	DebugWriter        io.Writer
	PageViewIDFunc     func() string
}

type SearchOptions struct {
	PostalCode       string
	ZoneID           string
	First            int
	OrderBy          string
	ItemsDisplayType string
	PageSource       string
	PageViewID       string
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

type itemsVariables struct {
	IDs        []string `json:"ids"`
	ShopID     string   `json:"shopId"`
	ZoneID     string   `json:"zoneId"`
	PostalCode string   `json:"postalCode,omitempty"`
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
		baseURL:            strings.TrimRight(baseURL, "/"),
		httpClient:         httpClient,
		instacartSID:       strings.TrimSpace(cfg.InstacartSID),
		instacartSessionID: strings.TrimSpace(cfg.InstacartSessionID),
		cookieHeader:       strings.TrimSpace(cfg.CookieHeader),
		debugWriter:        cfg.DebugWriter,
		pageViewIDFunc:     pageViewIDFunc,
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
	c.debugf(
		"graphql operation=%s shop_id=%s postal_code=%s zone_id=%s slug=%s first=%d order_by=%s items_display_type=%s page_source=%s page_view_id=%s",
		collectionProductsOperationName,
		storeID,
		strings.TrimSpace(opts.PostalCode),
		strings.TrimSpace(opts.ZoneID),
		categorySlug,
		resolvedFirst(opts.First),
		resolvedString(opts.OrderBy, defaultOrderBy),
		resolvedString(opts.ItemsDisplayType, defaultItemsDisplayType),
		resolvedString(opts.PageSource, defaultPageSource),
		pageViewID,
	)

	initCookies, err := c.initCookies(ctx)
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
	req.Header.Set("Referer", c.baseURL+"/store/aldi/collections/"+url.PathEscape(categorySlug))
	if c.cookieHeader != "" {
		req.Header.Set("Cookie", c.cookieHeader)
	} else if c.instacartSID != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-instacart_sid", Value: c.instacartSID})
	}
	if c.cookieHeader == "" && c.instacartSessionID != "" {
		req.AddCookie(&http.Cookie{Name: "_instacart_session_id", Value: c.instacartSessionID})
	}
	for _, cookie := range initCookies {
		req.AddCookie(cookie)
	}
	c.debugf("graphql request cookies=%s", cookieNames(req.Cookies()))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read collection products response: %w", readErr)
	}
	c.debugf("graphql status=%d body_preview=%s", resp.StatusCode, bodyPreview(body))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("collection products request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload CollectionProductsPayload
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode collection products response: %w", err)
	}
	c.debugf(
		"graphql decoded items=%d item_ids=%d featured_products=%d header=%q search_id=%q errors=%d",
		len(payload.Data.CollectionProducts.Items),
		len(payload.Data.ItemIDs()),
		len(payload.Data.CollectionProductsBasedSearchResults.ItemResultList.FeaturedProducts),
		payload.Data.CollectionProductsBasedSearchResults.ViewSection.HeaderString,
		payload.Data.CollectionProductsBasedSearchResults.SearchID,
		len(payload.Errors),
	)
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("collection products GraphQL error: %s", graphQLErrorsString(payload.Errors))
	}
	return &payload, nil
}

func (c *Client) Items(ctx context.Context, storeID string, ids []string, opts SearchOptions) (*ItemsPayload, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, errors.New("item ids are required")
	}

	pageViewID := strings.TrimSpace(opts.PageViewID)
	if pageViewID == "" {
		pageViewID = strings.TrimSpace(c.pageViewIDFunc())
	}
	if pageViewID == "" {
		return nil, errors.New("page view id is required")
	}

	endpoint, err := c.itemsURL(storeID, ids, opts)
	if err != nil {
		return nil, err
	}
	c.debugf(
		"graphql operation=%s shop_id=%s postal_code=%s zone_id=%s item_ids=%d page_view_id=%s",
		itemsOperationName,
		storeID,
		strings.TrimSpace(opts.PostalCode),
		strings.TrimSpace(opts.ZoneID),
		len(ids),
		pageViewID,
	)

	initCookies, err := c.initCookies(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build items request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Identifier", "web")
	req.Header.Set("X-IC-View-Layer", "true")
	req.Header.Set("X-Page-View-Id", pageViewID)
	req.Header.Set("Referer", c.baseURL+"/store/aldi/storefront")
	if c.cookieHeader != "" {
		req.Header.Set("Cookie", c.cookieHeader)
	} else if c.instacartSID != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-instacart_sid", Value: c.instacartSID})
	}
	if c.cookieHeader == "" && c.instacartSessionID != "" {
		req.AddCookie(&http.Cookie{Name: "_instacart_session_id", Value: c.instacartSessionID})
	}
	for _, cookie := range initCookies {
		req.AddCookie(cookie)
	}
	c.debugf("items request cookies=%s", cookieNames(req.Cookies()))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read items response: %w", readErr)
	}
	c.debugf("items status=%d body_preview=%s", resp.StatusCode, bodyPreview(body))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("items request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload ItemsPayload
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode items response: %w", err)
	}
	c.debugf("items decoded items=%d errors=%d", len(payload.Data.Items), len(payload.Errors))
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("items GraphQL error: %s", graphQLErrorsString(payload.Errors))
	}
	return &payload, nil
}

func (c *Client) initCookies(ctx context.Context) ([]*http.Cookie, error) {
	if c.cookieHeader != "" || c.instacartSID != "" || c.instacartSessionID != "" {
		c.debugf("init skipped manual_cookie_override=true cookie_header=%t instacart_sid=%t instacart_session_id=%t", c.cookieHeader != "", c.instacartSID != "", c.instacartSessionID != "")
		return nil, nil
	}

	endpoint := c.baseURL + "/idp/v1/init"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("build init request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", c.baseURL+"/store/aldi/storefront")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("init request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	cookies := resp.Cookies()
	c.debugf("init status=%d cookies=%s", resp.StatusCode, cookieNames(cookies))
	return cookies, nil
}

func (c *Client) collectionProductsURL(storeID, categorySlug, pageViewID string, opts SearchOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/graphql")
	if err != nil {
		return "", fmt.Errorf("parse collection products URL: %w", err)
	}

	variables := collectionProductsVariables{
		ShopID:           storeID,
		PostalCode:       strings.TrimSpace(opts.PostalCode),
		ZoneID:           resolvedZoneID(opts.ZoneID),
		Slug:             categorySlug,
		OrderBy:          resolvedString(opts.OrderBy, defaultOrderBy),
		Filters:          []string{},
		PageViewID:       pageViewID,
		ItemsDisplayType: resolvedString(opts.ItemsDisplayType, defaultItemsDisplayType),
		First:            resolvedFirst(opts.First),
		PageSource:       resolvedString(opts.PageSource, defaultPageSource),
	}
	rawVariables, err := json.Marshal(variables)
	if err != nil {
		return "", fmt.Errorf("marshal variables: %w", err)
	}

	extensions := persistedQueryExtensions{
		PersistedQuery: persistedQuery{
			Version:    1,
			SHA256Hash: collectionProductsPersistedQueryHash,
		},
	}
	rawExtensions, err := json.Marshal(extensions)
	if err != nil {
		return "", fmt.Errorf("marshal extensions: %w", err)
	}

	query := endpoint.Query()
	query.Set("operationName", collectionProductsOperationName)
	query.Set("variables", string(rawVariables))
	query.Set("extensions", string(rawExtensions))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *Client) itemsURL(storeID string, ids []string, opts SearchOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/graphql")
	if err != nil {
		return "", fmt.Errorf("parse items URL: %w", err)
	}

	variables := itemsVariables{
		IDs:        ids,
		ShopID:     storeID,
		ZoneID:     resolvedZoneID(opts.ZoneID),
		PostalCode: strings.TrimSpace(opts.PostalCode),
	}
	rawVariables, err := json.Marshal(variables)
	if err != nil {
		return "", fmt.Errorf("marshal items variables: %w", err)
	}

	extensions := persistedQueryExtensions{
		PersistedQuery: persistedQuery{
			Version:    1,
			SHA256Hash: itemsPersistedQueryHash,
		},
	}
	rawExtensions, err := json.Marshal(extensions)
	if err != nil {
		return "", fmt.Errorf("marshal items extensions: %w", err)
	}

	query := endpoint.Query()
	query.Set("operationName", itemsOperationName)
	query.Set("variables", string(rawVariables))
	query.Set("extensions", string(rawExtensions))
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func compactStrings(values []string) []string {
	compact := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		compact = append(compact, value)
	}
	return compact
}

func resolvedFirst(value int) int {
	if value == 0 {
		return defaultFirst
	}
	return value
}

func resolvedString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// ALDI's collection query requires a zone id, but the shop lookup APIs we have
// only give us the store/shop identifiers. Until we can derive the real zone
// from session location state, fall back to a random zone in the observed range.
func resolvedZoneID(value string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}

	const minZoneID = 100
	const maxZoneID = 300

	n := big.NewInt(maxZoneID - minZoneID + 1)
	v, err := rand.Int(rand.Reader, n)
	if err != nil {
		return "100"
	}
	return fmt.Sprintf("%d", minZoneID+int(v.Int64()))
}

func (c *Client) debugf(format string, args ...any) {
	if c.debugWriter == nil {
		return
	}
	_, _ = fmt.Fprintf(c.debugWriter, "aldiquery debug: "+format+"\n", args...)
}

func cookieNames(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return "(none)"
	}

	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		names = append(names, cookie.Name)
	}
	if len(names) == 0 {
		return "(none)"
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func bodyPreview(body []byte) string {
	const maxPreviewBytes = 2000
	preview := strings.TrimSpace(string(body))
	if len(preview) <= maxPreviewBytes {
		return preview
	}
	return preview[:maxPreviewBytes] + "...(truncated)"
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
