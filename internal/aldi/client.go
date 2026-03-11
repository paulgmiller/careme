package aldi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	Container        = "aldi"
	StoreCachePrefix = "aldi/stores/"
	LocationIDPrefix = "aldi_"
	DefaultBaseURL   = "https://locator.uberall.com"
	DefaultWidgetKey = "LETA2YVm6txbe0b9lS297XdxDX4qVQ" //what is this?
	DefaultLanguage  = "en_US"
	DefaultCountry   = "US"
)

type Client struct {
	BaseURL    string
	WidgetKey  string
	Language   string
	Country    string
	HTTPClient *http.Client
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
	ID         string   `json:"id"`
	StoreID    int64    `json:"store_id"`
	Identifier string   `json:"identifier"`
	Name       string   `json:"name"`
	Address    string   `json:"address"`
	City       string   `json:"city"`
	State      string   `json:"state"`
	ZipCode    string   `json:"zip_code"`
	Lat        *float64 `json:"lat,omitempty"`
	Lon        *float64 `json:"lon,omitempty"`
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
		WidgetKey:  widgetKey,
		Language:   DefaultLanguage,
		Country:    DefaultCountry,
		HTTPClient: httpClient,
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
