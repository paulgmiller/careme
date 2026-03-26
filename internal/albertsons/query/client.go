package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultSearchBaseURL = "https://www.safeway.com"
	defaultSearchPath    = "/abs/pub/xapi/wcax/pathway/search"
	defaultSearchRows    = 30                    // how high can we go.
	defaultSearchWidget  = "GR-C-Categ-6090cd27" // need to get
	defaultSearchDVID    = "web-4.1search"
	defaultSearchPGM     = "abs"
	defaultSearchUUID    = "null"
	defaultSearchChannel = "instore"
	defaultSearchUser    = "G"
)

type SearchClient struct {
	baseURL         string
	banner          string
	subscriptionKey string
	reese84         string
	visitorID       string
	httpClient      *http.Client
}

type SearchClientConfig struct {
	BaseURL         string
	Banner          string
	SubscriptionKey string
	Reese84         string
	VisitorID       string
	HTTPClient      *http.Client
}

type SearchOptions struct {
	Query     string
	Start     int
	Rows      int
	Sort      string
	WidgetID  string
	DVID      string
	VisitorID string
	UUID      string
	HouseID   string
	ClubCard  string
	UserType  string
}

type SearchResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
}

func (r *SearchResponse) DecodeJSON(dest any) error {
	if len(r.Body) == 0 {
		return errors.New("response body is empty")
	}
	if err := json.Unmarshal(r.Body, dest); err != nil {
		return fmt.Errorf("decode json response: %w", err)
	}
	return nil
}

type previousLoginCookie struct {
	Info previousLoginInfo `json:"info"`
}

type previousLoginInfo struct {
	Common previousLoginCommon `json:"COMMON"`
}

type previousLoginCommon struct {
	HouseID  string `json:"houseId,omitempty"`
	ClubCard string `json:"clubCard,omitempty"`
	UserType string `json:"userType,omitempty"`
	StoreID  string `json:"storeId"`
	ZipCode  string `json:"zipcode"`
}

func NewSearchClient(cfg SearchClientConfig) (*SearchClient, error) {
	subscriptionKey := strings.TrimSpace(cfg.SubscriptionKey)
	if subscriptionKey == "" {
		return nil, errors.New("subscription key is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultSearchBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	banner := strings.TrimSpace(cfg.Banner)
	if banner == "" {
		var err error
		banner, err = inferBanner(baseURL)
		if err != nil {
			return nil, err
		}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	visitorID := strings.TrimSpace(cfg.VisitorID)
	if visitorID == "" {
		visitorID = uuid.NewString()
	}

	return &SearchClient{
		baseURL:         baseURL,
		banner:          banner,
		subscriptionKey: subscriptionKey,
		reese84:         strings.TrimSpace(cfg.Reese84),
		visitorID:       visitorID,
		httpClient:      httpClient,
	}, nil
}

func (c *SearchClient) Search(ctx context.Context, storeID, zipCode string, opts SearchOptions) (*SearchResponse, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}

	zipCode = strings.TrimSpace(zipCode)
	if zipCode == "" {
		return nil, errors.New("zip code is required")
	}

	endpoint, err := url.Parse(c.baseURL + defaultSearchPath)
	if err != nil {
		return nil, fmt.Errorf("parse search URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("url", c.baseURL)
	query.Set("q", strings.TrimSpace(opts.Query))
	query.Set("rows", fmt.Sprintf("%d", normalizedRows(opts.Rows)))
	query.Set("start", fmt.Sprintf("%d", normalizedStart(opts.Start)))
	query.Set("channel", defaultSearchChannel)
	query.Set("storeid", storeID)
	query.Set("sort", strings.TrimSpace(opts.Sort))
	query.Set("widget-id", defaultString(opts.WidgetID, defaultSearchWidget))
	query.Set("dvid", defaultString(opts.DVID, defaultSearchDVID))
	query.Set("visitorId", defaultString(opts.VisitorID, c.visitorID))
	query.Set("uuid", defaultString(opts.UUID, defaultSearchUUID))
	query.Set("pgm", defaultSearchPGM)
	query.Set("includeOffer", "true")
	query.Set("banner", c.banner)
	query.Set("facet", "false")
	endpoint.RawQuery = query.Encode()

	log.Printf("search endpoint: %s", endpoint.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("ocp-apim-subscription-key", c.subscriptionKey)

	previousLogin, err := encodePreviousLoginCookie(storeID, zipCode, opts)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{Name: "ACI_S_abs_previouslogin", Value: previousLogin})
	req.AddCookie(&http.Cookie{Name: "reese84", Value: c.reese84})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("search request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return &SearchResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
	}, nil
}

func encodePreviousLoginCookie(storeID, zipCode string, opts SearchOptions) (string, error) {
	raw, err := json.Marshal(previousLoginCookie{
		Info: previousLoginInfo{
			Common: previousLoginCommon{
				HouseID:  strings.TrimSpace(opts.HouseID),
				ClubCard: strings.TrimSpace(opts.ClubCard),
				UserType: defaultString(opts.UserType, defaultSearchUser),
				StoreID:  storeID,
				ZipCode:  zipCode,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encode previous login cookie: %w", err)
	}
	return url.QueryEscape(string(raw)), nil
}

func inferBanner(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}

	host := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return "", fmt.Errorf("base URL %q is missing host", baseURL)
	}

	parts := strings.Split(host, ".")
	if len(parts) < 2 || parts[0] == "" {
		return "", fmt.Errorf("cannot infer banner from base URL %q", baseURL)
	}

	return parts[0], nil
}

func normalizedRows(rows int) int {
	if rows <= 0 {
		return defaultSearchRows
	}
	return rows
}

func normalizedStart(start int) int {
	if start < 0 {
		return 0
	}
	return start
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
