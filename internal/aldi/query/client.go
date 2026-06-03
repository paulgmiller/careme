package query

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

const (
	DefaultBaseURL = "https://www.aldi.us"

	collectionProductsOperationName = "CollectionProductsWithFeaturedProducts"
	itemsOperationName              = "Items"
	// without these query hashes you get error: query collection products: items GraphQL error: Error separating operations: expected exactly one operation, got 0
	// danger of changing obviously.
	collectionProductsPersistedQueryHash = "f3193dacfeec83828016dc1b3c8af8e61c4470d3f466da2d5797b3f2c530369c"
	itemsPersistedQueryHash              = "b2411f6acba21b2a6a277ca5616fdc5d1265ba647808895c05fe7ce1fd2fdcec"

	defaultFirst   = 10
	defaultOrderBy = "bestMatch"
	itemBatchSize  = 30
)

type Client struct {
	baseURL        string
	httpClient     *http.Client
	pageViewIDFunc func() string
	zoneMu         sync.Mutex
	zoneIDsByStore map[string]string
}

type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

type SearchOptions struct {
	First int
}

type collectionProductsVariables struct {
	ShopID     string   `json:"shopId"`
	PostalCode string   `json:"postalCode,omitempty"`
	ZoneID     string   `json:"zoneId,omitempty"`
	Slug       string   `json:"slug"`
	OrderBy    string   `json:"orderBy"`
	Filters    []string `json:"filters"`
	PageViewID string   `json:"pageViewId"`
	First      int      `json:"first"`
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

	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     httpClient,
		pageViewIDFunc: uuid.NewString,
		zoneIDsByStore: make(map[string]string),
	}
}

func (c *Client) Products(ctx context.Context, storeID, postalCode, categorySlug string, opts SearchOptions) ([]Item, error) {
	if err := validateCollectionProductsArgs(storeID, postalCode, categorySlug, opts); err != nil {
		return nil, err
	}
	storeID = strings.TrimSpace(storeID)
	postalCode = strings.TrimSpace(postalCode)
	categorySlug = strings.TrimSpace(categorySlug)

	// cache this for small period?
	initCookies, err := c.initCookies(ctx)
	if err != nil {
		return nil, err
	}

	pageViewID := strings.TrimSpace(c.pageViewIDFunc())

	payload, err := c.collectionProducts(ctx, storeID, postalCode, categorySlug, pageViewID, opts, initCookies)
	if err != nil {
		return nil, err
	}

	limit := resolvedFirst(opts.First)
	items := payload.Data.Items()
	itemIDs := payload.Data.ItemIDs()
	if len(itemIDs) <= len(items) {
		return items, nil
	}
	return c.hydrateItems(ctx, storeID, postalCode, pageViewID, itemIDs, opts, limit, initCookies)
}

func validateCollectionProductsArgs(storeID, postalCode, categorySlug string, opts SearchOptions) error {
	if strings.TrimSpace(storeID) == "" {
		return errors.New("store id is required")
	}
	if strings.TrimSpace(postalCode) == "" {
		return errors.New("postal code is required")
	}
	if strings.TrimSpace(categorySlug) == "" {
		return errors.New("category slug is required")
	}
	if opts.First < 0 {
		return errors.New("first must be greater than or equal to 0")
	}
	return nil
}

func (c *Client) hydrateItems(ctx context.Context, storeID, postalCode, pageView string, itemIDs []string, opts SearchOptions, limit int, initCookies []*http.Cookie) ([]Item, error) {
	items := make([]Item, 0, len(itemIDs))
	for _, batch := range lo.Chunk(itemIDs, itemBatchSize) {
		payload, err := c.items(ctx, storeID, postalCode, pageView, batch, opts, initCookies)
		if err != nil {
			return nil, err
		}
		items = append(items, payload.Data.Items...)
	}
	return items, nil
}

func (c *Client) collectionProducts(ctx context.Context, storeID, postalCode, categorySlug, pageViewID string, opts SearchOptions, initCookies []*http.Cookie) (*CollectionProductsPayload, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	postalCode = strings.TrimSpace(postalCode)
	if postalCode == "" {
		return nil, errors.New("postal code is required")
	}

	categorySlug = strings.TrimSpace(categorySlug)
	if categorySlug == "" {
		return nil, errors.New("category slug is required")
	}

	if opts.First < 0 {
		return nil, errors.New("first must be greater than or equal to 0")
	}

	endpoint, err := c.collectionProductsURL(storeID, postalCode, categorySlug, pageViewID, opts)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "aldi graphql request",
		"operation", collectionProductsOperationName, "shop_id", storeID,
		"postal_code", postalCode, "slug", categorySlug,
		"first", resolvedFirst(opts.First), "page_view_id", pageViewID,
	)

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
	for _, cookie := range initCookies {
		req.AddCookie(cookie)
	}
	slog.DebugContext(ctx, "aldi graphql request cookies", "operation", collectionProductsOperationName, "cookies", cookieNames(req.Cookies()))

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
	slog.DebugContext(ctx, "aldi graphql response", "operation", collectionProductsOperationName, "status", resp.StatusCode, "body_preview", bodyPreview(body))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("collection products request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload CollectionProductsPayload
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode collection products response: %w", err)
	}
	slog.DebugContext(
		ctx,
		"aldi graphql decoded",
		"operation", collectionProductsOperationName,
		"items", len(payload.Data.CollectionProducts.Items),
		"item_ids", len(payload.Data.ItemIDs()),
		"featured_products", len(payload.Data.CollectionProductsBasedSearchResults.ItemResultList.FeaturedProducts),
		"header", payload.Data.CollectionProductsBasedSearchResults.ViewSection.HeaderString,
		"search_id", payload.Data.CollectionProductsBasedSearchResults.SearchID,
		"errors", len(payload.Errors),
	)
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("collection products GraphQL error: %s", graphQLErrorsString(payload.Errors))
	}
	return &payload, nil
}

func (c *Client) items(ctx context.Context, storeID, postalCode, pageViewID string, ids []string, opts SearchOptions, initCookies []*http.Cookie) (*ItemsPayload, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	postalCode = strings.TrimSpace(postalCode)
	if postalCode == "" {
		return nil, errors.New("postal code is required")
	}

	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, errors.New("item ids are required")
	}

	endpoint, err := c.itemsURL(storeID, postalCode, ids, opts)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(
		ctx,
		"aldi graphql request",
		"operation", itemsOperationName,
		"shop_id", storeID,
		"postal_code", postalCode,
		"item_ids", len(ids),
		"page_view_id", pageViewID,
	)

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
	for _, cookie := range initCookies {
		req.AddCookie(cookie)
	}
	slog.DebugContext(ctx, "aldi graphql request cookies", "operation", itemsOperationName, "cookies", cookieNames(req.Cookies()))

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
	slog.DebugContext(ctx, "aldi graphql response", "operation", itemsOperationName, "status", resp.StatusCode, "body_preview", bodyPreview(body))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("items request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload ItemsPayload
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode items response: %w", err)
	}
	slog.DebugContext(ctx, "aldi graphql decoded", "operation", itemsOperationName, "items", len(payload.Data.Items), "errors", len(payload.Errors))
	if len(payload.Errors) > 0 {
		return nil, fmt.Errorf("items GraphQL error: %s", graphQLErrorsString(payload.Errors))
	}
	return &payload, nil
}

func (c *Client) initCookies(ctx context.Context) ([]*http.Cookie, error) {
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
	slog.DebugContext(ctx, "aldi init response", "status", resp.StatusCode, "cookies", cookieNames(cookies))
	return cookies, nil
}

func (c *Client) collectionProductsURL(storeID, postalCode, categorySlug, pageViewID string, opts SearchOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/graphql")
	if err != nil {
		return "", fmt.Errorf("parse collection products URL: %w", err)
	}

	variables := collectionProductsVariables{
		ShopID:     storeID,
		PostalCode: strings.TrimSpace(postalCode),
		ZoneID:     c.zoneIDForStore(storeID),
		Slug:       categorySlug,
		OrderBy:    defaultOrderBy,
		Filters:    []string{},
		PageViewID: pageViewID,
		First:      resolvedFirst(opts.First),
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

func (c *Client) itemsURL(storeID, postalCode string, ids []string, opts SearchOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/graphql")
	if err != nil {
		return "", fmt.Errorf("parse items URL: %w", err)
	}

	variables := itemsVariables{
		IDs:        ids,
		ShopID:     storeID,
		ZoneID:     c.zoneIDForStore(storeID),
		PostalCode: strings.TrimSpace(postalCode),
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

func (c *Client) zoneIDForStore(storeID string) string {
	storeID = strings.TrimSpace(storeID)
	c.zoneMu.Lock()
	defer c.zoneMu.Unlock()

	if zoneID := c.zoneIDsByStore[storeID]; zoneID != "" {
		return zoneID
	}
	zoneID := generatedZoneID()
	c.zoneIDsByStore[storeID] = zoneID
	return zoneID
}

func (c *Client) cacheZoneID(storeID, zoneID string) {
	if storeID == "" || zoneID == "" {
		return
	}
	c.zoneMu.Lock()
	defer c.zoneMu.Unlock()
	c.zoneIDsByStore[storeID] = zoneID
}

// ALDI's collection query requires a zone id, but the shop lookup APIs we have
// only give us the store/shop identifiers. Until we can derive the real zone
// from session location state, fall back to a random zone in the observed range.
func generatedZoneID() string {
	const minZoneID = 100
	const maxZoneID = 300

	n := big.NewInt(maxZoneID - minZoneID + 1)
	v, err := rand.Int(rand.Reader, n)
	if err != nil {
		return "100"
	}
	return fmt.Sprintf("%d", minZoneID+int(v.Int64()))
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
