package walmart

import (
	"bytes"
	"careme/internal/config"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// DefaultBaseURL is the Walmart Affiliates API base URL.
	DefaultBaseURL = "https://developer.api.walmart.com/api-proxy/service/affil/product/v2"
)

// Client calls Walmart Affiliates APIs with signed headers.
type Client struct {
	consumerID string
	keyVersion string
	privateKey *rsa.PrivateKey
	baseURL    string
	httpClient *http.Client
}

// StoresQuery controls store locator query parameters.
// Current implementation intentionally supports ZIP search only.
type StoresQuery struct {
	Lat  string
	Lon  string
	Zip  string
	City string
}

// NewClient creates a Walmart affiliates client.
func NewClient(cfg config.WalmartConfig) (*Client, error) {
	if strings.TrimSpace(cfg.ConsumerID) == "" {
		return nil, errors.New("consumer ID is required")
	}
	if strings.TrimSpace(cfg.KeyVersion) == "" {
		cfg.KeyVersion = "1"
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, errors.New("private key  is required")
	}

	privateKey, err := parseOpenSSHKeyFromEnv(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	return &Client{
		consumerID: cfg.ConsumerID,
		keyVersion: cfg.KeyVersion,
		privateKey: privateKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}, nil
}

// docshttps://walmart.io/docs/affiliates/v1/taxonomy
// example https://developer.api.walmart.com/api-proxy/service/affil/product/v2/taxonomy
func (c *Client) Taxonomy(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/taxonomy", nil)
	if err != nil {
		return nil, fmt.Errorf("build taxonomy request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	if err := c.applyAuthHeaders(req); err != nil {
		return nil, fmt.Errorf("apply walmart auth headers: %w", err)
	}

	slog.InfoContext(ctx, "searching Walmart taxonomy", "url", req.URL.String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request taxonomy: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read taxonomy response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("taxonomy request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("taxonomy request succeeded but response was not valid JSON: %s", strings.TrimSpace(string(body)))
	}

	return body, nil
}

// docs https://walmart.io/docs/affiliates/v1/stores
// example https://developer.api.walmart.com/api-proxy/service/affil/v2/stores?zip=98007
// example https://developer.api.walmart.com/api-proxy/service/affil/v2/stores?zip=77063
// SearchStoresByZIPData returns typed store locations for the provided ZIP code.
func (c *Client) SearchStoresByZIP(ctx context.Context, zip string) ([]Store, error) {
	zip = strings.TrimSpace(zip)
	if zip == "" {
		return nil, errors.New("zip code is required")
	}

	// Match Walmart ZIP sample path: /api-proxy/service/affil/v2/stores?zip=...
	params := url.Values{}
	params.Set("zip", zip)
	raw, err := c.searchStoresWithParams(ctx, params)
	if err != nil {
		return nil, err
	}
	stores, err := ParseStores(raw)
	if err != nil {
		return nil, fmt.Errorf("parse stores response: %w", err)
	}
	return stores, nil
}

func (c *Client) searchStoresWithParams(ctx context.Context, params url.Values) (json.RawMessage, error) {
	storesURL, err := url.Parse(c.baseURL + "/stores")
	if err != nil {
		return nil, fmt.Errorf("parse stores URL: %w", err)
	}
	storesURL.RawQuery = params.Encode()

	//slog.InfoContext(ctx, "searching Walmart stores", "url", storesURL.String())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, storesURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build stores request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	if err := c.applyAuthHeaders(req); err != nil {
		return nil, fmt.Errorf("apply walmart auth headers: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request stores: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, resp.Body) // ensure body is fully read for connection reuse
	if err != nil {
		return nil, fmt.Errorf("read stores response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		slog.ErrorContext(ctx, "received Walmart stores response", "status", resp.StatusCode)
		return nil, fmt.Errorf("stores request failed: status %d", resp.StatusCode) //, strings.TrimSpace(string(body)))
	}

	return buf.Bytes(), nil
}

func (c *Client) applyAuthHeaders(req *http.Request) error {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	signature, err := buildSignature(c.privateKey, c.consumerID, timestamp, c.keyVersion)
	if err != nil {
		return err
	}

	req.Header.Set("WM_CONSUMER.ID", c.consumerID)
	req.Header.Set("WM_CONSUMER.INTIMESTAMP", timestamp)
	req.Header.Set("WM_SEC.KEY_VERSION", c.keyVersion)
	req.Header.Set("WM_SEC.AUTH_SIGNATURE", signature)
	req.Header.Set("WM_QOS.CORRELATION_ID", randomCorrelationID())
	return nil
}

func buildSignature(privateKey *rsa.PrivateKey, consumerID, timestamp, keyVersion string) (string, error) {
	_, payload := canonicalize(map[string]string{
		"WM_CONSUMER.ID":          consumerID,
		"WM_CONSUMER.INTIMESTAMP": timestamp,
		"WM_SEC.KEY_VERSION":      keyVersion,
	})
	sum := sha256.Sum256([]byte(payload))

	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(sig), nil
}

func canonicalize(headersToSign map[string]string) (string, string) {
	keys := make([]string, 0, len(headersToSign))
	for key := range headersToSign {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var (
		parameterNames strings.Builder
		canonicalized  strings.Builder
	)
	for _, key := range keys {
		parameterNames.WriteString(strings.TrimSpace(key))
		parameterNames.WriteString(";")
		canonicalized.WriteString(strings.TrimSpace(headersToSign[key]))
		canonicalized.WriteString("\n")
	}

	return parameterNames.String(), canonicalized.String()
}

func randomCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("careme-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b[:])
}

func parseOpenSSHKeyFromEnv(b64 string) (*rsa.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("decode base64 key: %w", err)
	}
	key, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse OpenSSH key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, expected *rsa.PrivateKey", key)
	}
	return rsaKey, nil
}
