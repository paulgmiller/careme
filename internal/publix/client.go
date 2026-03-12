package publix

import (
	"bytes"
	"context"
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

const DefaultBaseURL = "https://www.publix.com"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type ProbeResult struct {
	StoreID string
	Exists  bool
	URL     string
}

type StoreSummary struct {
	ID      string   `json:"id"`
	StoreID string   `json:"store_id"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	City    string   `json:"city"`
	State   string   `json:"state"`
	ZipCode string   `json:"zip_code"`
	URL     string   `json:"url"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`
}

type storePage struct {
	StoreNumber int          `json:"storeNumber"`
	Name        string       `json:"name"`
	Address     storeAddress `json:"address"`
	Latitude    float64      `json:"latitude"`
	Longitude   float64      `json:"longitude"`
}

type storeAddress struct {
	StreetAddress string `json:"streetAddress"`
	City          string `json:"city"`
	State         string `json:"state"`
	Zip           string `json:"zip"`
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

	cloned := *httpClient
	cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &cloned,
	}
}

func (c *Client) ResolveStore(ctx context.Context, storeID string) (*ProbeResult, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, errors.New("store id is required")
	}
	if _, err := strconv.Atoi(storeID); err != nil {
		return nil, fmt.Errorf("store id %q must be numeric: %w", storeID, err)
	}

	endpoint := c.baseURL + "/locations/" + url.PathEscape(storeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build store probe request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint, err)
	}
	defer func() {
		//why do we need to copy to discard here?
		// Because some servers (including Cloudflare) will not close the connection
		// if the response body is not fully read, which can lead to resource leaks and
		// exhaustion of available connections in the HTTP client's connection pool.
		// By copying the remaining data to io.Discard, we ensure that the entire
		// response body is read and the connection can be properly reused or closed by the server.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest:
		location := strings.TrimSpace(resp.Header.Get("Location"))
		if location == "" {
			return nil, fmt.Errorf("redirect missing location header for store %s", storeID)
		}
		resolved, err := req.URL.Parse(location)
		if err != nil {
			return nil, fmt.Errorf("parse redirect location %q: %w", location, err)
		}
		if isMissingStoreRedirect(resolved) {
			return &ProbeResult{StoreID: storeID, Exists: false}, nil
		}
		if strings.HasPrefix(resolved.Path, "/locations/") {
			return &ProbeResult{StoreID: storeID, Exists: true, URL: resolved.String()}, nil
		}
		return nil, fmt.Errorf("unexpected redirect target %q for store %s", resolved.String(), storeID)
	case resp.StatusCode == http.StatusOK:
		return &ProbeResult{StoreID: storeID, Exists: true, URL: endpoint}, nil
	case resp.StatusCode == http.StatusNotFound:
		return &ProbeResult{StoreID: storeID, Exists: false}, nil
	default:
		return nil, fmt.Errorf("request %q: unexpected status %d", endpoint, resp.StatusCode)
	}
}

func (c *Client) StoreSummary(ctx context.Context, pageURL string) (*StoreSummary, error) {
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return nil, errors.New("page url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build store page request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", pageURL, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return ExtractStoreSummary(pageURL, body)
}

func ExtractStoreSummary(pageURL string, body []byte) (*StoreSummary, error) {
	payload, err := extractStorePayload(body)
	if err != nil {
		return nil, err
	}

	if payload.StoreNumber == 0 {
		return nil, errors.New("store number missing from publix store payload")
	}
	if strings.TrimSpace(payload.Name) == "" {
		return nil, errors.New("store name missing from publix store payload")
	}

	lat := payload.Latitude
	lon := payload.Longitude
	storeID := strconv.Itoa(payload.StoreNumber)

	return &StoreSummary{
		ID:      LocationIDPrefix + storeID,
		StoreID: storeID,
		Name:    strings.TrimSpace(payload.Name),
		Address: strings.TrimSpace(payload.Address.StreetAddress),
		City:    strings.TrimSpace(payload.Address.City),
		State:   strings.TrimSpace(payload.Address.State),
		ZipCode: normalizeZIP(payload.Address.Zip),
		URL:     strings.TrimSpace(pageURL),
		Lat:     &lat,
		Lon:     &lon,
	}, nil
}

func extractStorePayload(body []byte) (*storePage, error) {
	tokenizer := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			if err := tokenizer.Err(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil, errors.New("publix store payload not found")
				}
				return nil, fmt.Errorf("tokenize publix store page: %w", err)
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			for _, attr := range token.Attr {
				if attr.Key != ":store" {
					continue
				}

				raw := html.UnescapeString(attr.Val)
				var payload storePage
				if err := json.Unmarshal([]byte(raw), &payload); err != nil {
					return nil, fmt.Errorf("decode publix store payload: %w", err)
				}
				return &payload, nil
			}
		}
	}
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

func isMissingStoreRedirect(u *url.URL) bool {
	path := strings.TrimRight(strings.TrimSpace(u.Path), "/")
	return path == "/locations"
}
