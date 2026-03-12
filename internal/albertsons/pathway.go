package albertsons

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

const (
	DefaultAlbertsonsBaseURL = "https://www.safeway.com"
	defaultPathwayRows       = 120
	defaultBanner            = "safeway"
	defaultPathwayPath       = "/abs/pub/xapi/wcax/pathway/search"
)

type PathwayClient struct {
	baseURL    string
	httpClient *http.Client
}

type CuratedGridConfig struct {
	WidgetID        string
	ProductsPerPage int
	DVID            string
	SubscriptionKey string
	PathwayPath     string
}

type FetchCuratedProductsOptions struct {
	StoreID      string
	VisitorID    string
	UUID         string
	Banner       string
	CookieHeader string
	Rows         int
}

type PathwaySearchResponse struct {
	AppCode        string               `json:"appCode"`
	AppMsg         string               `json:"appMsg"`
	Response       PathwaySearchResults `json:"response"`
	DynamicFilters map[string]any       `json:"dynamic_filters"`
	OffersData     PathwayOffersData    `json:"offersData"`
}

type PathwaySearchResults struct {
	DisableTracking bool             `json:"disableTracking"`
	Docs            []PathwayProduct `json:"docs"`
	IsExactMatch    bool             `json:"isExactMatch"`
	NumFound        int              `json:"numFound"`
	Start           int              `json:"start"`
}

type PathwayProduct struct {
	ID                  string              `json:"id"`
	PID                 string              `json:"pid"`
	UPC                 string              `json:"upc"`
	StoreID             string              `json:"storeId"`
	Status              string              `json:"status"`
	Name                string              `json:"name"`
	ImageURL            string              `json:"imageUrl"`
	Price               *float64            `json:"price,omitempty"`
	BasePrice           *float64            `json:"basePrice,omitempty"`
	PricePer            *float64            `json:"pricePer,omitempty"`
	BasePricePer        *float64            `json:"basePricePer,omitempty"`
	PromoDescription    string              `json:"promoDescription,omitempty"`
	PromoText           string              `json:"promoText,omitempty"`
	PromoType           string              `json:"promoType,omitempty"`
	PromoEndDate        string              `json:"promoEndDate,omitempty"`
	DepartmentName      string              `json:"departmentName,omitempty"`
	AisleName           string              `json:"aisleName,omitempty"`
	AisleLocation       string              `json:"aisleLocation,omitempty"`
	ShelfName           string              `json:"shelfName,omitempty"`
	ShelfNameWithID     string              `json:"shelfNameWithId,omitempty"`
	UnitOfMeasure       string              `json:"unitOfMeasure,omitempty"`
	UnitQuantity        string              `json:"unitQuantity,omitempty"`
	DisplayUnitQtyText  string              `json:"displayUnitQuantityText,omitempty"`
	DisplayEstimateText string              `json:"displayEstimateText,omitempty"`
	SellByWeight        string              `json:"sellByWeight,omitempty"`
	AverageWeight       []string            `json:"averageWeight,omitempty"`
	InventoryAvailable  string              `json:"inventoryAvailable,omitempty"`
	ChannelEligibility  *ChannelEligibility `json:"channelEligibility,omitempty"`
	ChannelInventory    *ChannelInventory   `json:"channelInventory,omitempty"`
	Labels              []ProductLabel      `json:"labels,omitempty"`
	Warnings            []ProductWarning    `json:"warnings,omitempty"`
}

type ChannelEligibility struct {
	PickUp   bool `json:"pickUp"`
	Delivery bool `json:"delivery"`
	InStore  bool `json:"inStore"`
	Shipping bool `json:"shipping"`
}

type ChannelInventory struct {
	Delivery string `json:"delivery"`
	PickUp   string `json:"pickup"`
	InStore  string `json:"instore"`
	Shipping string `json:"shipping"`
}

type ProductLabel struct {
	LabelName string `json:"labelName"`
}

type ProductWarning struct {
	FoodIndicator   string `json:"foodIndicator"`
	WarnMsgTxt      string `json:"warnMsgTxt"`
	WarningSourceNm string `json:"warningSourceNm"`
}

type PathwayOffersData struct {
	Departments map[string]any          `json:"departments"`
	UPCs        map[string]PathwayOffer `json:"upcs"`
}

type PathwayOffer struct {
	WeeklyAdBadge bool           `json:"weeklyAdBadge"`
	Offers        map[string]any `json:"offers,omitempty"`
}

type searchConfigPayload struct {
	SubscriptionKey string `json:"apimSubscriptionKey"`
	PathwayPath     string `json:"apimPathwaySearchProductsEndpoint"`
}

func NewPathwayClient(httpClient *http.Client) *PathwayClient {
	return NewPathwayClientWithBaseURL(DefaultAlbertsonsBaseURL, httpClient)
}

func NewPathwayClientWithBaseURL(baseURL string, httpClient *http.Client) *PathwayClient {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultAlbertsonsBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &PathwayClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func ExtractCuratedGridConfig(page []byte) (*CuratedGridConfig, error) {
	searchGrid, err := extractSearchGrid(page)
	if err != nil {
		return nil, err
	}

	cfg := &CuratedGridConfig{
		WidgetID:        strings.TrimSpace(searchGrid["data-custom-control-id"]),
		DVID:            normalizeDVID(searchGrid["data-dvid-config"]),
		PathwayPath:     defaultPathwayPath,
		ProductsPerPage: parseProductsPerPage(searchGrid["data-products-per-page"]),
	}
	if cfg.ProductsPerPage <= 0 {
		cfg.ProductsPerPage = 30
	}
	if cfg.WidgetID == "" {
		return nil, errors.New("search-grid widget id not found")
	}
	if cfg.DVID == "" {
		return nil, errors.New("search-grid dvid config not found")
	}

	payload, err := extractSearchConfig(page)
	if err != nil {
		return nil, err
	}
	cfg.SubscriptionKey = strings.TrimSpace(payload.SubscriptionKey)
	if cfg.SubscriptionKey == "" {
		return nil, errors.New("search config subscription key not found")
	}
	if strings.TrimSpace(payload.PathwayPath) != "" {
		cfg.PathwayPath = strings.TrimSpace(payload.PathwayPath)
	}
	return cfg, nil
}

func (c *PathwayClient) FetchCuratedGridConfig(ctx context.Context, pageURL string, cookieHeader string) (*CuratedGridConfig, error) {
	body, err := c.fetchPage(ctx, pageURL, cookieHeader)
	if err != nil {
		return nil, err
	}
	return ExtractCuratedGridConfig(body)
}

func (c *PathwayClient) FetchCuratedProducts(ctx context.Context, pageURL string, opts FetchCuratedProductsOptions) ([]PathwayProduct, error) {
	if strings.TrimSpace(opts.StoreID) == "" {
		return nil, errors.New("store id is required")
	}

	cfg, err := c.FetchCuratedGridConfig(ctx, pageURL, opts.CookieHeader)
	if err != nil {
		return nil, err
	}

	rows := opts.Rows
	if rows <= 0 || rows > defaultPathwayRows {
		rows = defaultPathwayRows
	}

	visitorID := strings.TrimSpace(opts.VisitorID)
	if visitorID == "" {
		visitorID = randomUUID()
	}

	sessionUUID := strings.TrimSpace(opts.UUID)
	if sessionUUID == "" {
		sessionUUID = randomUUID()
	}

	banner := strings.TrimSpace(opts.Banner)
	if banner == "" {
		banner = defaultBanner
	}

	var all []PathwayProduct
	total := -1

	for start := 0; ; start += rows {
		page, err := c.searchCuratedPage(ctx, pageURL, cfg, curatedSearchRequest{
			StoreID:      opts.StoreID,
			VisitorID:    visitorID,
			UUID:         sessionUUID,
			Banner:       banner,
			CookieHeader: opts.CookieHeader,
			Rows:         rows,
			Start:        start,
		})
		if err != nil {
			return nil, err
		}

		if total < 0 {
			total = page.Response.NumFound
			all = make([]PathwayProduct, 0, minPositive(total, rows))
		}
		if len(page.Response.Docs) == 0 {
			break
		}

		all = append(all, page.Response.Docs...)
		if len(all) >= total || len(page.Response.Docs) < rows {
			break
		}
	}

	if total >= 0 && len(all) > total {
		all = all[:total]
	}
	return all, nil
}

type curatedSearchRequest struct {
	StoreID      string
	VisitorID    string
	UUID         string
	Banner       string
	CookieHeader string
	Rows         int
	Start        int
}

func (c *PathwayClient) searchCuratedPage(ctx context.Context, pageURL string, cfg *CuratedGridConfig, req curatedSearchRequest) (*PathwaySearchResponse, error) {
	endpoint, err := buildPathwaySearchURL(pageURL, cfg, req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build pathway request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json, text/plain, */*")
	httpReq.Header.Set("Referer", strings.TrimSpace(pageURL))
	httpReq.Header.Set("User-Agent", "Mozilla/5.0")
	httpReq.Header.Set("ocp-apim-subscription-key", cfg.SubscriptionKey)
	if strings.TrimSpace(req.CookieHeader) != "" {
		httpReq.Header.Set("Cookie", req.CookieHeader)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read pathway response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("pathway request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded PathwaySearchResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode pathway response: %w", err)
	}
	return &decoded, nil
}

func buildPathwaySearchURL(pageURL string, cfg *CuratedGridConfig, req curatedSearchRequest) (string, error) {
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil {
		return "", fmt.Errorf("parse page url: %w", err)
	}

	pathwayURL := u.Scheme + "://" + u.Host + cfg.PathwayPath
	endpoint, err := url.Parse(pathwayURL)
	if err != nil {
		return "", fmt.Errorf("parse pathway url: %w", err)
	}

	params := endpoint.Query()
	params.Set("request-id", strconv.FormatInt(time.Now().UnixNano(), 10))
	params.Set("url", u.Scheme+"://"+u.Host)
	params.Set("search-uid", "")
	params.Set("q", "")
	params.Set("rows", strconv.Itoa(req.Rows))
	params.Set("start", strconv.Itoa(req.Start))
	params.Set("channel", "instore")
	params.Set("storeid", strings.TrimSpace(req.StoreID))
	params.Set("sort", "")
	params.Set("widget-id", cfg.WidgetID)
	params.Set("dvid", cfg.DVID)
	params.Set("visitorId", strings.TrimSpace(req.VisitorID))
	params.Set("uuid", strings.TrimSpace(req.UUID))
	params.Set("pgm", "abs")
	params.Set("includeOffer", "true")
	params.Set("banner", strings.TrimSpace(req.Banner))
	params.Set("facet", "false")
	endpoint.RawQuery = params.Encode()
	return endpoint.String(), nil
}

func (c *PathwayClient) fetchPage(ctx context.Context, pageURL string, cookieHeader string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(pageURL), nil)
	if err != nil {
		return nil, fmt.Errorf("build page request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if strings.TrimSpace(cookieHeader) != "" {
		req.Header.Set("Cookie", cookieHeader)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", pageURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read page response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("page request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func extractSearchGrid(page []byte) (map[string]string, error) {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(string(page)))
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if err := tokenizer.Err(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil, errors.New("search-grid element not found")
				}
				return nil, fmt.Errorf("tokenize search-grid: %w", err)
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "search-grid" {
				continue
			}

			attrs := make(map[string]string, len(token.Attr))
			for _, attr := range token.Attr {
				attrs[attr.Key] = html.UnescapeString(attr.Val)
			}
			return attrs, nil
		}
	}
}

func extractSearchConfig(page []byte) (*searchConfigPayload, error) {
	const prefix = "SWY.CONFIGSERVICE.initSearchConfig('"
	const suffix = "');"

	text := string(page)
	start := strings.Index(text, prefix)
	if start < 0 {
		return nil, errors.New("search config payload not found")
	}
	start += len(prefix)

	end := strings.Index(text[start:], suffix)
	if end < 0 {
		return nil, errors.New("search config payload terminator not found")
	}

	raw := text[start : start+end]
	var payload searchConfigPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("decode search config payload: %w", err)
	}
	return &payload, nil
}

func parseProductsPerPage(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func normalizeDVID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasSuffix(raw, "search") {
		return raw
	}
	return raw + "search"
}

func randomUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}

	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	var out [36]byte
	hex.Encode(out[0:8], buf[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], buf[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], buf[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], buf[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], buf[10:16])
	return string(out[:])
}

func minPositive(a, b int) int {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}
