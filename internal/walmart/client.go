package walmart

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// DefaultBaseURL is the Walmart Affiliates API base URL.
	DefaultBaseURL = "https://developer.api.walmart.com/api-proxy/service/affil/product/v2"
)

// Config defines the required Walmart affiliate credentials and client options.
type Config struct {
	ConsumerID     string
	KeyVersion     string
	PrivateKeyPath string
	BaseURL        string
	HTTPClient     *http.Client
}

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
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.ConsumerID) == "" {
		return nil, errors.New("consumer ID is required")
	}
	if strings.TrimSpace(cfg.KeyVersion) == "" {
		cfg.KeyVersion = "1"
	}
	if strings.TrimSpace(cfg.PrivateKeyPath) == "" {
		return nil, errors.New("private key path is required")
	}

	privateKey, err := LoadRSAPrivateKey(cfg.PrivateKeyPath)
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
	defer resp.Body.Close()

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
func (c *Client) SearchStoresByZIP(ctx context.Context, zip string) (json.RawMessage, error) {
	if zip == "" {
		return nil, errors.New("zip code is required")
	}

	// Match Walmart ZIP sample path: /api-proxy/service/affil/v2/stores?zip=...
	params := url.Values{}
	params.Set("zip", zip)
	return c.searchStoresWithParams(ctx, params)
}

func (c *Client) searchStoresWithParams(ctx context.Context, params url.Values) (json.RawMessage, error) {
	storesURL, err := url.Parse(c.baseURL + "/stores")
	if err != nil {
		return nil, fmt.Errorf("parse stores URL: %w", err)
	}
	storesURL.RawQuery = params.Encode()

	slog.InfoContext(ctx, "searching Walmart stores", "url", storesURL.String())

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
	defer resp.Body.Close()

	io.Copy(os.Stdout, resp.Body) // ensure body is fully read for connection reuse

	slog.InfoContext(ctx, "received Walmart stores response", "status", resp.StatusCode)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("stores request failed: status %d", resp.StatusCode) //, strings.TrimSpace(string(body)))
	}

	return []byte{}, nil
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
	slog.Info("signing payload", "payload", payload)
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

// LoadRSAPrivateKey loads a PEM (PKCS1/PKCS8) or OpenSSH private key file.
func LoadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	if k, err := parsePEMPrivateKey(content); err == nil {
		return k, nil
	}

	rawKey, err := ssh.ParseRawPrivateKey(content)
	if err == nil {
		rsaKey, ok := rawKey.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is %T, expected *rsa.PrivateKey", rawKey)
		}
		return rsaKey, nil
	}

	// Some Walmart examples provide private key as raw base64 PKCS#8 content.
	rsaKey, err := parseBase64PKCS8PrivateKey(content)
	if err == nil {
		return rsaKey, nil
	}

	return nil, fmt.Errorf("parse private key: %w", err)
}

func parseBase64PKCS8PrivateKey(content []byte) (*rsa.PrivateKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(content)))
	if err != nil {
		return nil, fmt.Errorf("decode base64 key: %w", err)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(decoded)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#8 key: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, expected *rsa.PrivateKey", parsed)
	}
	return rsaKey, nil
}

func parsePEMPrivateKey(content []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, errors.New("not PEM encoded")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PEM key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, expected *rsa.PrivateKey", parsed)
	}
	return key, nil
}
