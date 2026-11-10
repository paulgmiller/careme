package wegmans

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
	"time"
)

const (
	Container        = "wegmans"
	StoreCachePrefix = "wegmans/stores/"
	LocationIDPrefix = "wegmans_"
	DefaultBaseURL   = "https://www.wegmans.com"
)

var ErrStoreNotFound = errors.New("wegmans store not found")

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type StoreResponse struct {
	ID                int     `json:"id"`
	StoreNumber       int     `json:"storeNumber"`
	Name              string  `json:"name"`
	City              string  `json:"city"`
	StateAbbreviation string  `json:"stateAbbreviation"`
	Zip               string  `json:"zip"`
	StreetAddress     string  `json:"streetAddress"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
}

type StoreSummary struct {
	ID          string   `json:"id"`
	StoreNumber int      `json:"store_number"`
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	City        string   `json:"city"`
	State       string   `json:"state"`
	ZipCode     string   `json:"zip_code"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

func NewClient(httpClient *http.Client) *Client {
	return NewClientWithBaseURL(DefaultBaseURL, httpClient)
}

func NewClientWithBaseURL(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) StoreSummary(ctx context.Context, storeNumber int) (*StoreSummary, error) {
	if storeNumber < 0 {
		return nil, fmt.Errorf("store number %d must be non-negative", storeNumber)
	}

	endpoint := c.baseURL + "/api/stores/store-number/" + url.PathEscape(strconv.Itoa(storeNumber))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrStoreNotFound
	}
	if resp.StatusCode == http.StatusInternalServerError {
		// dumb but this seems to be true
		return nil, ErrStoreNotFound
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch %s: status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload StoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	slog.Info("fetched wegmans store summary", "store_number", storeNumber, "name", payload.Name)
	summary, err := normalizeStore(payload)
	if err != nil {
		return nil, fmt.Errorf("normalize wegmans store %d: %w", storeNumber, err)
	}
	return summary, nil
}

func normalizeStore(payload StoreResponse) (*StoreSummary, error) {
	storeNumber := payload.StoreNumber
	if storeNumber == 0 {
		storeNumber = payload.ID
	}
	if storeNumber == 0 {
		return nil, errors.New("missing store number")
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, errors.New("missing store name")
	}

	address := strings.TrimSpace(payload.StreetAddress)
	if address == "" {
		return nil, errors.New("missing street address")
	}

	state := strings.ToUpper(strings.TrimSpace(payload.StateAbbreviation))
	if state == "" {
		return nil, errors.New("missing state abbreviation")
	}

	zipCode := normalizeZIP(payload.Zip)
	if zipCode == "" {
		return nil, errors.New("missing zip code")
	}

	summary := &StoreSummary{
		ID:          LocationIDPrefix + strconv.Itoa(storeNumber),
		StoreNumber: storeNumber,
		Name:        normalizeName(name),
		Address:     address,
		City:        strings.TrimSpace(payload.City),
		State:       state,
		ZipCode:     zipCode,
	}

	if payload.Latitude != 0 && payload.Longitude != 0 {
		lat := payload.Latitude
		lon := payload.Longitude
		summary.Lat = &lat
		summary.Lon = &lon
	}

	return summary, nil
}

func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Wegmans"
	}
	if strings.HasPrefix(strings.ToLower(name), "wegmans") {
		return name
	}
	return "Wegmans " + name
}

func normalizeZIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if dash := strings.IndexByte(raw, '-'); dash >= 0 {
		raw = raw[:dash]
	}
	if len(raw) > 5 {
		raw = raw[:5]
	}
	return raw
}
