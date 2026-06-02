package aldi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Container        = "aldi"
	StoreCachePrefix = "aldi/stores/"
	LocationIDPrefix = "aldi_"
	DefaultBaseURL   = "https://locator.uberall.com"
	DefaultShopURL   = "https://www.aldi.us"
	DefaultWidgetKey = "LETA2YVm6txbe0b9lS297XdxDX4qVQ" // what is this?
	DefaultLanguage  = "en_US"
	DefaultCountry   = "US"
)

type Client struct {
	BaseURL    string
	ShopURL    string
	WidgetKey  string
	Language   string
	Country    string
	HTTPClient *http.Client

	shopCookies []*http.Cookie
	shopsByZip  map[string][]Shop
}

type SourceLocation struct {
	ID              int64   `json:"id"`
	Identifier      string  `json:"identifier"`
	Name            string  `json:"name"`
	StreetAndNumber string  `json:"streetAndNumber"`
	AddressExtra    *string `json:"addressExtra"`
	City            string  `json:"city"`
	Province        string  `json:"province"`
	Zip             string  `json:"zip"`
	Lat             float64 `json:"lat"`
	Lng             float64 `json:"lng"`
}

type StoreSummary struct {
	ID            string   `json:"id"`
	StoreID       int64    `json:"store_id"`
	Identifier    string   `json:"identifier"`
	InstoreShopID string   `json:"instore_shop_id,omitempty"`
	Name          string   `json:"name"`
	Address       string   `json:"address"`
	City          string   `json:"city"`
	State         string   `json:"state"`
	ZipCode       string   `json:"zip_code"`
	Lat           *float64 `json:"lat,omitempty"`
	Lon           *float64 `json:"lon,omitempty"`
}

type Shop struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	RetailerKey       string      `json:"retailer_key"`
	PhoneNumber       string      `json:"phone_number"`
	FulfillmentOption string      `json:"fulfillment_option"`
	Address           ShopAddress `json:"address"`
	RetailerLogoURL   string      `json:"retailer_logo_url"`
	BackgroundColor   string      `json:"background_color_hex"`
	LocationName      string      `json:"location_name"`
	LocationCode      string      `json:"location_code"`
}

type ShopAddress struct {
	StreetAddress string `json:"street_address"`
	City          string `json:"city"`
	State         string `json:"state"`
	PostalCode    string `json:"postal_code"`
	CountryCode   string `json:"country_code"`
}

type shopsResponse struct {
	Shops []Shop `json:"shops"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

type allLocationsResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Response struct {
		Locations []SourceLocation `json:"locations"`
	} `json:"response"`
}

func NewClient(httpClient *http.Client) *Client {
	return NewClientWithBaseURL(DefaultBaseURL, DefaultWidgetKey, httpClient)
}

func NewClientWithBaseURL(baseURL, widgetKey string, httpClient *http.Client) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	widgetKey = strings.TrimSpace(widgetKey)
	if widgetKey == "" {
		widgetKey = DefaultWidgetKey
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		ShopURL:    DefaultShopURL,
		WidgetKey:  widgetKey,
		Language:   DefaultLanguage,
		Country:    DefaultCountry,
		HTTPClient: httpClient,
		shopsByZip: make(map[string][]Shop),
	}
}

func (c *Client) AllLocations(ctx context.Context) ([]SourceLocation, error) {
	if strings.TrimSpace(c.WidgetKey) == "" {
		return nil, errors.New("widget key is required")
	}

	endpoint, err := url.Parse(c.BaseURL + "/api/locators/" + url.PathEscape(c.WidgetKey) + "/locations/all")
	if err != nil {
		return nil, fmt.Errorf("parse locations URL: %w", err)
	}

	params := endpoint.Query()
	params.Set("language", valueOrDefault(c.Language, DefaultLanguage))
	params.Set("country", valueOrDefault(c.Country, DefaultCountry))
	endpoint.RawQuery = params.Encode()

	var response allLocationsResponse
	if err := c.getJSON(ctx, endpoint.String(), &response); err != nil {
		return nil, err
	}
	return response.Response.Locations, nil
}

func (c *Client) StoreSummaries(ctx context.Context) ([]*StoreSummary, error) {
	locations, err := c.AllLocations(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]*StoreSummary, 0, len(locations))
	for _, location := range locations {
		summary, err := normalizeLocation(location)
		if err != nil {
			return nil, fmt.Errorf("normalize ALDI location %s: %w", locationIdentifier(location), err)
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (c *Client) InStoreShopID(ctx context.Context, summary *StoreSummary) (string, error) {
	if summary == nil {
		return "", errors.New("store summary is required")
	}

	shops, err := c.ShopsByPostalCode(ctx, summary.ZipCode)
	if err != nil {
		return "", err
	}

	shop, ok := findInStoreShop(summary, shops)
	if !ok {
		return "", fmt.Errorf("instore shop not found for %s %s", summary.ID, summary.Address)
	}
	return shop.ID, nil
}

func (c *Client) ShopsByPostalCode(ctx context.Context, postalCode string) ([]Shop, error) {
	postalCode = strings.TrimSpace(postalCode)
	if postalCode == "" {
		return nil, errors.New("postal code is required")
	}
	if shops, ok := c.shopsByZip[postalCode]; ok {
		return shops, nil
	}

	if err := c.ensureShopSession(ctx); err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(strings.TrimRight(c.ShopURL, "/") + "/idp/v1/shops")
	if err != nil {
		return nil, fmt.Errorf("parse shops URL: %w", err)
	}
	params := endpoint.Query()
	params.Set("postal_code", postalCode)
	endpoint.RawQuery = params.Encode()

	var response shopsResponse
	if err := c.shopJSON(ctx, http.MethodGet, endpoint.String(), nil, &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("shops lookup failed: %s", strings.TrimSpace(response.Error.Message))
	}
	c.shopsByZip[postalCode] = response.Shops
	return response.Shops, nil
}

func (c *Client) ensureShopSession(ctx context.Context) error {
	if len(c.shopCookies) > 0 {
		return nil
	}

	endpoint := strings.TrimRight(c.ShopURL, "/") + "/idp/v1/init"
	return c.shopJSON(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")), nil)
}

func (c *Client) shopJSON(ctx context.Context, method, endpoint string, body io.Reader, dest any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build shop request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", strings.TrimRight(c.ShopURL, "/")+"/store/aldi/storefront")
	for _, cookie := range c.shopCookies {
		req.AddCookie(cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if cookies := resp.Cookies(); len(cookies) > 0 {
		c.shopCookies = mergeCookies(c.shopCookies, cookies)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("fetch %s: status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if dest == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

func mergeCookies(existing, next []*http.Cookie) []*http.Cookie {
	byName := make(map[string]*http.Cookie, len(existing)+len(next))
	for _, cookie := range existing {
		byName[cookie.Name] = cookie
	}
	for _, cookie := range next {
		byName[cookie.Name] = cookie
	}

	merged := make([]*http.Cookie, 0, len(byName))
	for _, cookie := range byName {
		merged = append(merged, cookie)
	}
	return merged
}

func findInStoreShop(summary *StoreSummary, shops []Shop) (Shop, bool) {
	for _, shop := range shops {
		if !strings.EqualFold(strings.TrimSpace(shop.RetailerKey), Container) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(shop.FulfillmentOption), "instore") {
			continue
		}
		if normalizeComparable(summary.Address) != normalizeComparable(shop.Address.StreetAddress) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(summary.City), strings.TrimSpace(shop.Address.City)) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(summary.State), strings.TrimSpace(shop.Address.State)) {
			continue
		}
		return shop, true
	}
	return Shop{}, false
}

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeComparable(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonAlphaNumeric.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeLocation(location SourceLocation) (*StoreSummary, error) {
	identifier := strings.TrimSpace(location.Identifier)
	if identifier == "" {
		return nil, errors.New("missing identifier")
	}

	address := joinAddress(location.StreetAndNumber, valueOrEmpty(location.AddressExtra))
	if address == "" {
		return nil, errors.New("missing street address")
	}

	state := normalizeState(location.Province)
	if state == "" {
		return nil, errors.New("missing province")
	}

	zipCode := strings.TrimSpace(location.Zip)
	if zipCode == "" {
		return nil, errors.New("missing zip code")
	}

	name := strings.TrimSpace(location.Name)
	if name == "" || strings.EqualFold(name, "ALDI") {
		name = "ALDI"
	}
	if name == "ALDI" {
		name = strings.TrimSpace(name + " " + address)
	}

	summary := &StoreSummary{
		ID:         LocationIDPrefix + identifier,
		StoreID:    location.ID,
		Identifier: identifier,
		Name:       name,
		Address:    address,
		City:       strings.TrimSpace(location.City),
		State:      state,
		ZipCode:    zipCode,
	}

	if location.Lat != 0 && location.Lng != 0 {
		lat := location.Lat
		lon := location.Lng
		summary.Lat = &lat
		summary.Lon = &lon
	}

	return summary, nil
}

func joinAddress(streetAndNumber, addressExtra string) string {
	address := strings.TrimSpace(streetAndNumber)
	extra := strings.TrimSpace(addressExtra)
	if address == "" {
		return extra
	}
	if extra == "" {
		return address
	}
	if strings.Contains(strings.ToLower(address), strings.ToLower(extra)) {
		return address
	}
	return address + ", " + extra
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func locationIdentifier(location SourceLocation) string {
	if strings.TrimSpace(location.Identifier) != "" {
		return strings.TrimSpace(location.Identifier)
	}
	if location.ID != 0 {
		return strconv.FormatInt(location.ID, 10)
	}
	return "unknown"
}

func (c *Client) getJSON(ctx context.Context, endpoint string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("fetch %s: status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}
