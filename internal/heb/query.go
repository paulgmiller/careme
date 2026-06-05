package heb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	defaultPageDelay    = 5 * time.Second
	categoryPageSize    = 50
)

// QueryClient fetches HEB category products from the Next.js data endpoint.
type QueryClient struct {
	baseURL    string
	httpClient *http.Client
	maxPages   int

	buildIDMu   sync.Mutex
	buildID     string
	loadBuildID loadBuildID

	// is this stopping bot blockcing? unclear
	pageDelay             time.Duration
	categoryRequestMu     sync.Mutex
	lastCategoryRequestAt time.Time
}

type QueryClientConfig struct {
	BaseURL     string
	BuildID     string
	LoadBuildID loadBuildID
	HTTPClient  *http.Client
	MaxPages    int
	PageDelay   time.Duration
}

type CategoryOptions struct {
	Reese84  string
	StoreID  string
	ParentID string
	ChildID  string
	// can produce some of this by above two ids?
	CategoryPath string

	Page  int
	Limit int

	// may not be necessary
	Int     string
	Referer string
}

type CategoryPage struct {
	Products []Product `json:"products"`
	Page     int       `json:"page"`
}

type CategoryHTTPError struct {
	StatusCode int
	Body       string
}

func (e *CategoryHTTPError) Error() string {
	return fmt.Sprintf("category request failed: status %d: %s", e.StatusCode, e.Body)
}

type Product struct {
	TypeName               string            `json:"__typename"`
	ID                     string            `json:"id"`
	StoreID                int               `json:"storeId"`
	ShoppingContext        any               `json:"shoppingContext"`
	DisplayName            string            `json:"displayName"`
	DecodedDisplayName     string            `json:"decodedDisplayName"`
	FullDisplayName        string            `json:"fullDisplayName"`
	FullCategoryHierarchy  string            `json:"fullCategoryHierarchy"`
	MinimumOrderQuantity   float32           `json:"minimumOrderQuantity"`
	MaximumOrderQuantity   float32           `json:"maximumOrderQuantity"`
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
	SKUs                   []SKU             `json:"SKUs"`
	ListPrice              *float32          `json:"-"`
	SalePrice              *float32          `json:"-"`
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

type SKU struct {
	ID                   string      `json:"id"`
	ContextPrices        []ItemPrice `json:"contextPrices"`
	CustomerFriendlySize string      `json:"customerFriendlySize"`
	TwelveDigitUPC       string      `json:"twelveDigitUPC"`
	TypeName             string      `json:"__typename"`
}

type ItemPrice struct {
	Context       string        `json:"context"`
	IsOnSale      bool          `json:"isOnSale"`
	IsPriceCut    bool          `json:"isPriceCut"`
	PriceType     string        `json:"priceType"`
	ListPrice     *DisplayPrice `json:"listPrice"`
	SalePrice     *DisplayPrice `json:"salePrice"`
	UnitListPrice *DisplayPrice `json:"unitListPrice"`
	UnitSalePrice *DisplayPrice `json:"unitSalePrice"`
	TypeName      string        `json:"__typename"`
}

type DisplayPrice struct {
	Unit            string   `json:"unit"`
	FormattedAmount string   `json:"formattedAmount"`
	Amount          *float32 `json:"amount"`
	TypeName        string   `json:"__typename"`
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
	pageDelay := cfg.PageDelay
	if pageDelay == 0 {
		pageDelay = defaultPageDelay
	}
	if pageDelay < 0 {
		pageDelay = 0
	}

	buildID := strings.TrimSpace(cfg.BuildID)

	return &QueryClient{
		baseURL:     baseURL,
		buildID:     buildID,
		loadBuildID: cfg.LoadBuildID,
		httpClient:  httpClient,
		maxPages:    maxPages,
		pageDelay:   pageDelay,
	}
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
	referer := strings.TrimSpace(opts.Referer)
	for pagesFetched := 0; pagesFetched < c.maxPages; pagesFetched++ {
		pageOpts := opts
		pageOpts.Page = page
		pageOpts.Referer = referer

		staleBuildID := c.currentBuildID()
		resp, err := c.CategoryPage(ctx, pageOpts)
		if pagesFetched == 0 && isCategoryNotFound(err) {
			if _, refreshErr := c.refreshBuildID(ctx, pageOpts, staleBuildID); refreshErr != nil {
				return nil, refreshErr
			}
			resp, err = c.CategoryPage(ctx, pageOpts)
		}
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
		if len(resp.Products) < categoryPageSize {
			slog.InfoContext(ctx, "category pagination stopped after short page", "page", page, "count", len(resp.Products), "page_size", categoryPageSize)
			return products, nil
		}

		referer = c.categoryPageURL(pageOpts, pageOpts.Page > 1)
		page++
	}

	return products, fmt.Errorf("category pagination exceeded max pages %d", c.maxPages)
}

func isCategoryNotFound(err error) bool {
	var httpErr *CategoryHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
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

	if err := c.waitForCategoryRequestSlot(ctx); err != nil {
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
		return nil, &CategoryHTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	products, err := decodeCategoryPagePayload(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}

	return &CategoryPage{Products: products, Page: opts.Page}, nil
}

func (c *QueryClient) waitForCategoryRequestSlot(ctx context.Context) error {
	if c.pageDelay <= 0 {
		return nil
	}

	c.categoryRequestMu.Lock()
	defer c.categoryRequestMu.Unlock()

	if !c.lastCategoryRequestAt.IsZero() {
		wait := c.pageDelay - time.Since(c.lastCategoryRequestAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	c.lastCategoryRequestAt = time.Now()
	return nil
}

func (c *QueryClient) resolveBuildID(ctx context.Context, opts CategoryOptions) (string, error) {
	c.buildIDMu.Lock()
	buildID := c.buildID
	c.buildIDMu.Unlock()
	if buildID != "" {
		return buildID, nil
	}
	return c.refreshBuildID(ctx, opts, "")
}

func (c *QueryClient) refreshBuildID(ctx context.Context, opts CategoryOptions, staleBuildID string) (string, error) {
	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()

	if current := c.buildID; current != "" && current != staleBuildID {
		slog.InfoContext(ctx, "using heb next data build id refreshed by another request", "build_id", current)
		return current, nil
	}
	if c.loadBuildID == nil {
		return "", errors.New("heb build id loader is required")
	}

	buildID, err := c.loadBuildID(ctx)
	if err != nil {
		return "", fmt.Errorf("discover heb build id: %w", err)
	}
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return "", fmt.Errorf("discover heb build id: empty build id")
	}
	c.buildID = buildID
	slog.InfoContext(ctx, "updated heb next data build id", "build_id", buildID)
	return buildID, nil
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
	req.Header.Set("Referer", c.categoryReferer(opts))
	req.Header.Set("X-Nextjs-Data", "1")
}

func (c *QueryClient) categoryReferer(opts CategoryOptions) string {
	if referer := strings.TrimSpace(opts.Referer); referer != "" {
		return referer
	}
	return c.categoryPageURL(opts, opts.Page > 1)
}

func (c *QueryClient) categoryPageURL(opts CategoryOptions, includePagination bool) string {
	rawURL := c.baseURL + c.categoryPagePath(opts)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	if includePagination {
		query.Set("page", strconv.Itoa(opts.Page))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
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

func decodeCategoryPagePayload(r io.Reader) ([]Product, error) {
	var payload categoryResponse
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode category json response: %w", err)
	}

	return payload.products(), nil
}

type categoryResponse struct {
	PageProps categoryPageProps `json:"pageProps"`
}

type categoryPageProps struct {
	Layout categoryLayout `json:"layout"`
}

type categoryLayout struct {
	VisualComponents []categoryProductCollection `json:"visualComponents"`
}

type categoryProductCollection struct {
	Items []Product `json:"items"`
}

func (r categoryResponse) products() []Product {
	seen := make(map[string]struct{})
	return collectProducts(nil, r.PageProps, seen)
}

func collectProducts(products []Product, page categoryPageProps, seen map[string]struct{}) []Product {
	for _, component := range page.Layout.VisualComponents {
		products = appendProducts(products, component.Items, seen)
	}
	return products
}

func appendProducts(products []Product, candidates []Product, seen map[string]struct{}) []Product {
	for _, product := range candidates {
		products = appendProduct(products, product, seen)
	}
	return products
}

func productPrices(product Product) (*float32, *float32) {
	for _, context := range []string{"CURBSIDE", "ONLINE", ""} {
		for _, sku := range product.SKUs {
			for _, price := range sku.ContextPrices {
				if context != "" && !strings.EqualFold(price.Context, context) {
					continue
				}
				listPrice := displayPriceAmount(price.ListPrice)
				salePrice := displayPriceAmount(price.SalePrice)
				if listPrice != nil || salePrice != nil {
					return listPrice, salePrice
				}
			}
		}
	}
	return nil, nil
}

func displayPriceAmount(price *DisplayPrice) *float32 {
	if price == nil {
		return nil
	}
	return price.Amount
}

func appendProduct(products []Product, product Product, seen map[string]struct{}) []Product {
	id := strings.TrimSpace(product.ID)
	if id == "" || !isDecodedProduct(product) {
		return products
	}
	if _, ok := seen[id]; ok {
		return products
	}
	seen[id] = struct{}{}
	product.ListPrice, product.SalePrice = productPrices(product)
	return append(products, product)
}

func isDecodedProduct(product Product) bool {
	if strings.EqualFold(product.TypeName, "Product") {
		return true
	}
	return strings.TrimSpace(product.DisplayName) != "" ||
		strings.TrimSpace(product.FullDisplayName) != "" ||
		strings.TrimSpace(product.DecodedDisplayName) != ""
}
