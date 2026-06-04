package heb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	DefaultBaseURL = "https://www.heb.com"

	defaultQueryTimeout = 20 * time.Second
	defaultMaxPages     = 20
)

var ErrBuildIDRequired = errors.New("heb next data build id is required")

// QueryClient fetches HEB category products from the Next.js data endpoint.
type QueryClient struct {
	baseURL    string
	httpClient *http.Client
	maxPages   int

	buildIDMu sync.Mutex
	buildID   string
}

type QueryClientConfig struct {
	BaseURL    string
	BuildID    string
	HTTPClient *http.Client
	MaxPages   int
}

type CategoryOptions struct {
	Reese84      string
	StoreID      string
	ParentID     string
	ChildID      string
	CategoryPath string
	Int          string
	Page         int
	Limit        int
}

type CategoryPage struct {
	Products []Product       `json:"products"`
	Page     int             `json:"page"`
	Raw      json.RawMessage `json:"-"`
}

type CategoryHTTPError struct {
	StatusCode int
	Body       string
}

func (e *CategoryHTTPError) Error() string {
	return fmt.Sprintf("category request failed: status %d: %s", e.StatusCode, e.Body)
}

type Product struct {
	ID                     string            `json:"id"`
	StoreID                int               `json:"storeId"`
	ShoppingContext        any               `json:"shoppingContext"`
	DisplayName            string            `json:"displayName"`
	DecodedDisplayName     string            `json:"decodedDisplayName"`
	FullDisplayName        string            `json:"fullDisplayName"`
	FullCategoryHierarchy  string            `json:"fullCategoryHierarchy"`
	MinimumOrderQuantity   int               `json:"minimumOrderQuantity"`
	MaximumOrderQuantity   int               `json:"maximumOrderQuantity"`
	BestAvailable          bool              `json:"bestAvailable"`
	OnAd                   bool              `json:"onAd"`
	IsNew                  bool              `json:"isNew"`
	PricedByWeight         bool              `json:"pricedByWeight"`
	ShowCouponFlag         bool              `json:"showCouponFlag"`
	InAssortment           bool              `json:"inAssortment"`
	IsEBTSnapProduct       bool              `json:"isEbtSnapProduct"`
	ProductLocation        *ProductLocation  `json:"productLocation"`
	PastPurchaseInfo       any               `json:"pastPurchaseInfo"`
	PurchasePreferenceList any               `json:"purchasePreferenceList"`
	Inventory              *Inventory        `json:"inventory"`
	Brand                  *Brand            `json:"brand"`
	ProductCategory        *ProductCategory  `json:"productCategory"`
	ProductImageURLs       []ProductImageURL `json:"productImageUrls"`
	Raw                    json.RawMessage   `json:"-"`
	Extra                  map[string]any    `json:"-"`
}

type ProductLocation struct {
	Location string `json:"location"`
	TypeName string `json:"__typename"`
}

type Inventory struct {
	InventoryState string `json:"inventoryState"`
	TypeName       string `json:"__typename"`
}

type Brand struct {
	Name       string `json:"name"`
	IsOwnBrand bool   `json:"isOwnBrand"`
	TypeName   string `json:"__typename"`
}

type ProductCategory struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TypeName string `json:"__typename"`
}

type ProductImageURL struct {
	URL      string `json:"url"`
	TypeName string `json:"__typename"`
}

type nextData struct {
	BuildID string `json:"buildId"`
}

var (
	nextStaticBuildIDRe = regexp.MustCompile(`/_next/static/([^/]+)/`)
	nextDataBuildIDRe   = regexp.MustCompile(`/_next/data/([^/]+)/`)
)

func NewQueryClient(cfg QueryClientConfig) *QueryClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultQueryTimeout}
	}

	maxPages := cfg.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}

	buildID := strings.TrimSpace(cfg.BuildID)

	return &QueryClient{
		baseURL:    baseURL,
		buildID:    buildID,
		httpClient: httpClient,
		maxPages:   maxPages,
	}
}

func (c *QueryClient) SetBuildID(buildID string) {
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return
	}

	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()
	c.buildID = buildID
}

func (c *QueryClient) currentBuildID() string {
	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()
	return c.buildID
}

func (c *QueryClient) Category(ctx context.Context, opts CategoryOptions) ([]Product, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	page := opts.Page
	if page <= 0 {
		page = 1
	}

	seenProducts := make(map[string]struct{})
	products := make([]Product, 0)
	for pagesFetched := 0; pagesFetched < c.maxPages; pagesFetched++ {
		pageOpts := opts
		pageOpts.Page = page

		resp, err := c.CategoryPage(ctx, pageOpts)
		if err != nil {
			if pagesFetched > 0 {
				slog.InfoContext(ctx, "category pagination stopped after page error", "page", page, "error", err)
				return products, nil
			}
			return nil, err
		}
		newProducts := appendNewProducts(&products, resp.Products, seenProducts, opts.Limit)
		if len(resp.Products) == 0 {
			slog.InfoContext(ctx, "category pagination stopped after empty page", "page", page)
			return products, nil
		}
		if newProducts == 0 && pagesFetched > 0 {
			slog.InfoContext(ctx, "category pagination stopped after duplicate page", "page", page)
			return products, nil
		}
		if opts.Limit > 0 && len(products) >= opts.Limit {
			slog.InfoContext(ctx, "category pagination stopped after limit", "page", page, "limit", opts.Limit)
			return products, nil
		}

		page++
	}

	return products, fmt.Errorf("category pagination exceeded max pages %d", c.maxPages)
}

func appendNewProducts(products *[]Product, candidates []Product, seen map[string]struct{}, limit int) int {
	var appended int
	for _, product := range candidates {
		if limit > 0 && len(*products) >= limit {
			break
		}
		id := strings.TrimSpace(product.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		*products = append(*products, product)
		appended++
	}
	return appended
}

func (c *QueryClient) CategoryPage(ctx context.Context, opts CategoryOptions) (*CategoryPage, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}

	buildID, err := c.resolveBuildID(ctx, opts)
	if err != nil {
		return nil, err
	}

	endpoint, err := c.categoryDataURL(buildID, opts)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build category request: %w", err)
	}
	c.setCategoryHeaders(req, opts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &CategoryHTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read category response: %w", err)
	}

	products, err := decodeCategoryPayload(body)
	if err != nil {
		return nil, err
	}

	return &CategoryPage{
		Products: products,
		Page:     opts.Page,
		Raw:      body,
	}, nil
}

func (c *QueryClient) resolveBuildID(ctx context.Context, opts CategoryOptions) (string, error) {
	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()

	if c.buildID != "" {
		return c.buildID, nil
	}

	return "", ErrBuildIDRequired
}

func (c *QueryClient) categoryDataURL(buildID string, opts CategoryOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/_next/data/" + url.PathEscape(buildID) + "/en/category/shop/" + url.PathEscape(opts.ParentID) + "/" + url.PathEscape(opts.ChildID) + ".json")
	if err != nil {
		return "", fmt.Errorf("parse category data URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("page", strconv.Itoa(opts.Page))
	query.Set("parentId", opts.ParentID)
	query.Set("childId", opts.ChildID)
	if intValue := strings.TrimSpace(opts.Int); intValue != "" {
		query.Set("int", intValue)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *QueryClient) setCategoryHeaders(req *http.Request, opts CategoryOptions) {
	c.setStoreCookies(req, opts)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", c.baseURL+c.categoryPagePath(opts))
	req.Header.Set("X-Nextjs-Data", "1")
}

func (c *QueryClient) categoryPagePath(opts CategoryOptions) string {
	if path := normalizeCategoryPath(opts.CategoryPath); path != "" {
		return path
	}
	return "/category/shop/" + url.PathEscape(opts.ParentID) + "/" + url.PathEscape(opts.ChildID)
}

func (c *QueryClient) setStoreCookies(req *http.Request, opts CategoryOptions) {
	req.AddCookie(&http.Cookie{Name: "reese84", Value: strings.TrimSpace(opts.Reese84)})
	req.AddCookie(&http.Cookie{Name: "SHOPPING_STORE_ID", Value: strings.TrimSpace(opts.StoreID)})
	req.AddCookie(&http.Cookie{Name: "CURR_SESSION_STORE", Value: strings.TrimSpace(opts.StoreID)})
	req.AddCookie(&http.Cookie{Name: "USER_CHOSEN_STORE", Value: "true"})
}

func (opts CategoryOptions) validate() error {
	if strings.TrimSpace(opts.Reese84) == "" {
		return errors.New("reese84 token is required")
	}
	if strings.TrimSpace(opts.StoreID) == "" {
		return errors.New("store id is required")
	}
	if strings.TrimSpace(opts.ParentID) == "" {
		return errors.New("parent category id is required")
	}
	if strings.TrimSpace(opts.ChildID) == "" {
		return errors.New("child category id is required")
	}
	return nil
}

func extractBuildID(body []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("parse category page html: %w", err)
	}

	var script string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if script != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "script" && queryAttrValue(n, "id") == "__NEXT_DATA__" {
			if n.FirstChild != nil {
				script = n.FirstChild.Data
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if strings.TrimSpace(script) != "" {
		var data nextData
		if err := json.Unmarshal([]byte(htmlstd.UnescapeString(script)), &data); err != nil {
			return "", fmt.Errorf("decode next data json: %w", err)
		}
		if strings.TrimSpace(data.BuildID) != "" {
			return strings.TrimSpace(data.BuildID), nil
		}
	}

	matches := nextStaticBuildIDRe.FindSubmatch(body)
	if len(matches) == 2 && strings.TrimSpace(string(matches[1])) != "" {
		return strings.TrimSpace(string(matches[1])), nil
	}

	matches = nextDataBuildIDRe.FindSubmatch(body)
	if len(matches) == 2 && strings.TrimSpace(string(matches[1])) != "" {
		return strings.TrimSpace(string(matches[1])), nil
	}

	return "", errors.New("next data build id not found")
}

func normalizeCategoryPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		value = parsed.Path
	}
	value = "/" + strings.Trim(value, "/")
	if !strings.HasPrefix(value, "/category/shop/") {
		return ""
	}
	return value
}

func queryAttrValue(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func decodeCategoryPayload(body []byte) ([]Product, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode category json response: %w, %s", err, body)
	}

	products, err := extractProducts(root)
	if err != nil {
		return nil, err
	}
	return products, nil
}

func extractProducts(root any) ([]Product, error) {
	var products []Product
	seen := make(map[string]struct{})
	walkQueryJSON(root, func(value any) {
		if arr, ok := value.([]any); ok {
			for _, product := range productsFromArray(arr) {
				products = appendProduct(products, product, seen)
			}
			return
		}
		if obj, ok := value.(map[string]any); ok && looksLikeProduct(obj) {
			product, ok := productFromObject(obj)
			if ok {
				products = appendProduct(products, product, seen)
			}
		}
	})
	if products == nil {
		return nil, nil
	}
	return products, nil
}

func productsFromArray(values []any) []Product {
	products := make([]Product, 0, len(values))
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok || !looksLikeProduct(obj) {
			continue
		}
		if product, ok := productFromObject(obj); ok {
			products = append(products, product)
		}
	}
	return products
}

func productFromObject(obj map[string]any) (Product, bool) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return Product{}, false
	}
	var product Product
	if err := json.Unmarshal(raw, &product); err != nil {
		return Product{}, false
	}
	if strings.TrimSpace(product.ID) == "" {
		return Product{}, false
	}
	product.Raw = raw
	return product, true
}

func appendProduct(products []Product, product Product, seen map[string]struct{}) []Product {
	id := strings.TrimSpace(product.ID)
	if id == "" {
		return products
	}
	if _, ok := seen[id]; ok {
		return products
	}
	seen[id] = struct{}{}
	return append(products, product)
}

func looksLikeProduct(obj map[string]any) bool {
	_, hasID := obj["id"]
	if !hasID {
		return false
	}
	if _, ok := obj["displayName"]; ok {
		return true
	}
	if _, ok := obj["fullDisplayName"]; ok {
		return true
	}
	if _, ok := obj["decodedDisplayName"]; ok {
		return true
	}
	return false
}

func walkQueryJSON(value any, visit func(any)) {
	visit(value)
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			walkQueryJSON(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkQueryJSON(child, visit)
		}
	}
}
