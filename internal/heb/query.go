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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://www.heb.com"

	defaultQueryTimeout = 20 * time.Second
	defaultPageDelay    = 5 * time.Second
)

// QueryClient fetches HEB category products from the Next.js data endpoint.
type QueryClient struct {
	baseURL    string
	httpClient *http.Client

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
	Int                string
	Referer            string
	SearchContextToken string
}

type CategoryPage struct {
	Products           []Product `json:"products"`
	Page               int       `json:"page"`
	SearchContextToken string    `json:"searchContextToken,omitempty"`
	Total              int       `json:"total,omitempty"`
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

func NewQueryClient(cfg QueryClientConfig) *QueryClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultQueryTimeout}
	}

	buildID := strings.TrimSpace(cfg.BuildID)

	return &QueryClient{
		baseURL:     baseURL,
		buildID:     buildID,
		loadBuildID: cfg.LoadBuildID,
		httpClient:  httpClient,
		pageDelay:   defaultPageDelay,
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

	page := max(opts.Page, 1)

	products := make([]Product, 0)
	referer := strings.TrimSpace(opts.Referer)
	searchContextToken := strings.TrimSpace(opts.SearchContextToken)
	firstFetch := true
	for {
		pageOpts := opts
		pageOpts.Page = page
		pageOpts.Referer = referer
		pageOpts.SearchContextToken = searchContextToken

		staleBuildID := c.currentBuildID()
		resp, err := c.categoryPage(ctx, pageOpts)

		// can we only get a build id change at start?
		if firstFetch && isCategoryNotFound(err) {
			if _, refreshErr := c.refreshBuildID(ctx, staleBuildID); refreshErr != nil {
				return nil, refreshErr
			}
			resp, err = c.categoryPage(ctx, pageOpts)
		}
		if err != nil {
			if !firstFetch {
				slog.WarnContext(ctx, "category pagination stopped after page error", "page", page, "error", err)
				return products, nil
			}
			return nil, err
		}
		firstFetch = false
		if len(resp.Products) == 0 {
			slog.InfoContext(ctx, "category pagination stopped after empty page", "page", page)
			return products, nil
		}
		products = append(products, resp.Products...)

		if len(products) >= opts.Limit {
			slog.InfoContext(ctx, "category pagination stopped after limit", "page", page, "limit", opts.Limit)
			return products, nil
		}
		if resp.Total > 0 && len(products) >= resp.Total {
			slog.InfoContext(ctx, "category pagination stopped after total", "page", page, "total", resp.Total)
			return products, nil
		}

		referer = c.categoryPageURL(pageOpts, pageOpts.Page > 1)
		searchContextToken = strings.TrimSpace(resp.SearchContextToken)
		page++
	}
}

func isCategoryNotFound(err error) bool {
	var httpErr *CategoryHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func (c *QueryClient) categoryPage(ctx context.Context, opts CategoryOptions) (*CategoryPage, error) {
	if opts.Page <= 0 {
		return nil, errors.New("page must be positive")
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

	// magic number limit reader
	return decodeCategoryPagePayload(io.LimitReader(resp.Body, 8*1024*1024), opts.Page)
}

// can we do this smarter? is it even necessary?
// if we query many stores from same server don't we bust anyways?
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
	return c.refreshBuildID(ctx, "")
}

func (c *QueryClient) refreshBuildID(ctx context.Context, staleBuildID string) (string, error) {
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
	if searchContextToken := strings.TrimSpace(opts.SearchContextToken); searchContextToken != "" {
		query.Set("sct", searchContextToken)
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
	if searchContextToken := strings.TrimSpace(opts.SearchContextToken); searchContextToken != "" {
		query.Set("sct", searchContextToken)
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
	if opts.Limit <= 0 {
		return errors.New("need a positive limit")
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

func decodeCategoryPagePayload(r io.Reader, page int) (*CategoryPage, error) {
	var payload categoryResponse
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode category json response: %w", err)
	}

	var products []Product
	for _, component := range payload.PageProps.Layout.VisualComponents {
		for _, product := range component.Items {
			if strings.TrimSpace(product.ID) == "" {
				continue
			}
			product.ListPrice, product.SalePrice = productPrices(product)
			products = append(products, product)
		}
	}

	return &CategoryPage{
		Products:           products,
		SearchContextToken: payload.PageProps.searchContextToken(),
		Page:               page,
		Total:              payload.PageProps.total(),
	}, nil
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
	Items              []Product `json:"items"`
	SearchContextToken string    `json:"searchContextToken"`
	Total              int       `json:"total"`
}

// a little worry that searchContextToken and total just pick first non blank one. Should at least pick same one?
func (p categoryPageProps) searchContextToken() string {
	for _, component := range p.Layout.VisualComponents {
		if searchContextToken := strings.TrimSpace(component.SearchContextToken); searchContextToken != "" {
			return searchContextToken
		}
	}
	return ""
}

func (p categoryPageProps) total() int {
	for _, component := range p.Layout.VisualComponents {
		if component.Total > 0 {
			return component.Total
		}
	}
	return 0
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

// Are there non products in item?
