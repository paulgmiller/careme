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
	defaultBaseURL = "https://www.heb.com"

	defaultQueryTimeout = 20 * time.Second
)

// queryClient fetches HEB category products from the Next.js data endpoint.
type queryClient struct {
	baseURL    string
	httpClient *http.Client

	buildIDMu   sync.Mutex
	buildID     string
	loadBuildID loadBuildID
}

type queryClientConfig struct {
	BaseURL     string
	LoadBuildID loadBuildID
	HTTPClient  *http.Client
}

type categoryOptions struct {
	Reese84            string
	StoreID            string
	ParentID           string
	ChildID            string
	Page               int
	Limit              int
	SearchContextToken string
}

type categoryPage struct {
	Products           []product `json:"products"`
	Page               int       `json:"page"`
	SearchContextToken string    `json:"searchContextToken,omitempty"`
	Total              int       `json:"total,omitempty"`
}

type categoryHTTPError struct {
	StatusCode int
	Body       string
}

func (e *categoryHTTPError) Error() string {
	return fmt.Sprintf("category request failed: status %d: %s", e.StatusCode, e.Body)
}

type product struct {
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
	ProductLocation        *productLocation  `json:"productLocation"`
	PastPurchaseInfo       any               `json:"pastPurchaseInfo"`
	PurchasePreferenceList any               `json:"purchasePreferenceList"`
	Inventory              *inventory        `json:"inventory"`
	Brand                  *brand            `json:"brand"`
	ProductCategory        *productCategory  `json:"productCategory"`
	ProductImageURLs       []productImageURL `json:"productImageUrls"`
	SKUs                   []sku             `json:"SKUs"`
	ListPrice              *float32          `json:"-"`
	SalePrice              *float32          `json:"-"`
	Size                   string            `json:"-"`
}

type productLocation struct {
	Location string `json:"location"`
	TypeName string `json:"__typename"`
}

type inventory struct {
	InventoryState string `json:"inventoryState"`
	TypeName       string `json:"__typename"`
}

type brand struct {
	Name       string `json:"name"`
	IsOwnBrand bool   `json:"isOwnBrand"`
	TypeName   string `json:"__typename"`
}

type productCategory struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TypeName string `json:"__typename"`
}

type productImageURL struct {
	URL      string `json:"url"`
	TypeName string `json:"__typename"`
}

type sku struct {
	ID                   string      `json:"id"`
	ContextPrices        []itemPrice `json:"contextPrices"`
	CustomerFriendlySize string      `json:"customerFriendlySize"`
	TwelveDigitUPC       string      `json:"twelveDigitUPC"`
	TypeName             string      `json:"__typename"`
}

type itemPrice struct {
	Context       string        `json:"context"`
	IsOnSale      bool          `json:"isOnSale"`
	IsPriceCut    bool          `json:"isPriceCut"`
	PriceType     string        `json:"priceType"`
	ListPrice     *displayPrice `json:"listPrice"`
	SalePrice     *displayPrice `json:"salePrice"`
	UnitListPrice *displayPrice `json:"unitListPrice"`
	UnitSalePrice *displayPrice `json:"unitSalePrice"`
	TypeName      string        `json:"__typename"`
}

type displayPrice struct {
	Unit            string   `json:"unit"`
	FormattedAmount string   `json:"formattedAmount"`
	Amount          *float32 `json:"amount"`
	TypeName        string   `json:"__typename"`
}

func newQueryClient(cfg queryClientConfig) *queryClient {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultQueryTimeout}
	}

	return &queryClient{
		baseURL:     baseURL,
		loadBuildID: cfg.LoadBuildID,
		httpClient:  httpClient,
	}
}

func (c *queryClient) currentBuildID() string {
	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()
	return c.buildID
}

func (c *queryClient) Category(ctx context.Context, opts categoryOptions) ([]product, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	page := max(opts.Page, 1)

	products := make([]product, 0)
	searchContextToken := strings.TrimSpace(opts.SearchContextToken)
	firstFetch := true
	for {
		pageOpts := opts
		pageOpts.Page = page
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

		searchContextToken = strings.TrimSpace(resp.SearchContextToken)
		page++
	}
}

func isCategoryNotFound(err error) bool {
	var httpErr *categoryHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func (c *queryClient) categoryPage(ctx context.Context, opts categoryOptions) (*categoryPage, error) {
	if opts.Page <= 0 {
		return nil, errors.New("page must be positive")
	}

	buildID, err := c.resolveBuildID(ctx)
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
		return nil, &categoryHTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	// magic number limit reader
	return decodeCategoryPagePayload(io.LimitReader(resp.Body, 8*1024*1024), opts.Page)
}

func (c *queryClient) resolveBuildID(ctx context.Context) (string, error) {
	c.buildIDMu.Lock()
	buildID := c.buildID
	c.buildIDMu.Unlock()
	if buildID != "" {
		return buildID, nil
	}
	return c.refreshBuildID(ctx, "")
}

func (c *queryClient) refreshBuildID(ctx context.Context, staleBuildID string) (string, error) {
	c.buildIDMu.Lock()
	defer c.buildIDMu.Unlock()

	if current := c.buildID; current != staleBuildID {
		slog.InfoContext(ctx, "using heb next data build id refreshed by another request", "build_id", current)
		return current, nil
	}
	buildID, err := c.loadBuildID(ctx, staleBuildID)
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

func (c *queryClient) categoryDataURL(buildID string, opts categoryOptions) (string, error) {
	endpoint, err := url.Parse(c.baseURL + "/_next/data/" + url.PathEscape(buildID) + "/en/category/shop/" + url.PathEscape(opts.ParentID) + "/" + url.PathEscape(opts.ChildID) + ".json")
	if err != nil {
		return "", fmt.Errorf("parse category data URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("page", strconv.Itoa(opts.Page))
	query.Set("parentId", opts.ParentID)
	query.Set("childId", opts.ChildID)
	if searchContextToken := strings.TrimSpace(opts.SearchContextToken); searchContextToken != "" {
		query.Set("sct", searchContextToken)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *queryClient) setCategoryHeaders(req *http.Request, opts categoryOptions) {
	c.setStoreCookies(req, opts)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("X-Nextjs-Data", "1")
}

func (c *queryClient) setStoreCookies(req *http.Request, opts categoryOptions) {
	req.AddCookie(&http.Cookie{Name: "reese84", Value: strings.TrimSpace(opts.Reese84)})
	req.AddCookie(&http.Cookie{Name: "SHOPPING_STORE_ID", Value: strings.TrimSpace(opts.StoreID)})
	req.AddCookie(&http.Cookie{Name: "CURR_SESSION_STORE", Value: strings.TrimSpace(opts.StoreID)})
	req.AddCookie(&http.Cookie{Name: "USER_CHOSEN_STORE", Value: "true"})
}

func (opts categoryOptions) validate() error {
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

func decodeCategoryPagePayload(r io.Reader, page int) (*categoryPage, error) {
	var payload categoryResponse
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode category json response: %w", err)
	}

	var products []product
	for _, component := range payload.PageProps.Layout.VisualComponents {
		for _, product := range component.Items {
			if strings.TrimSpace(product.ID) == "" {
				continue
			}
			product.ListPrice, product.SalePrice, product.Size = productPricing(product)
			products = append(products, product)
		}
	}

	return &categoryPage{
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
	Items              []product `json:"items"`
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

func productPricing(product product) (*float32, *float32, string) {
	for _, context := range []string{"CURBSIDE", "ONLINE", ""} {
		for _, sku := range product.SKUs {
			for _, price := range sku.ContextPrices {
				if context != "" && !strings.EqualFold(price.Context, context) {
					continue
				}
				listPrice := displayPriceAmount(price.ListPrice)
				salePrice := displayPriceAmount(price.SalePrice)
				if listPrice != nil || salePrice != nil {
					return listPrice, salePrice, strings.TrimSpace(sku.CustomerFriendlySize)
				}
			}
		}
	}
	return nil, nil, ""
}

func displayPriceAmount(price *displayPrice) *float32 {
	if price == nil {
		return nil
	}
	return price.Amount
}

// Are there non products in item?
