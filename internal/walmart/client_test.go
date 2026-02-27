package walmart

import (
	"careme/internal/config"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalize_SortsAndFormatsLikeJavaExample(t *testing.T) {
	t.Parallel()

	keys, values := canonicalize(map[string]string{
		"WM_SEC.KEY_VERSION":      "1 ",
		"WM_CONSUMER.INTIMESTAMP": " 12345",
		"WM_CONSUMER.ID":          " abc ",
	})

	if keys != "WM_CONSUMER.ID;WM_CONSUMER.INTIMESTAMP;WM_SEC.KEY_VERSION;" {
		t.Fatalf("unexpected key order: %q", keys)
	}
	if values != "abc\n12345\n1\n" {
		t.Fatalf("unexpected canonicalized values: %q", values)
	}
}

func TestSearchStoresByZIP_SetsHeadersAndQuery(t *testing.T) {
	t.Parallel()

	privateKey, encodedKey := newBase64RSAPrivateKey(t)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`{"results":[{"no":1,"name":"Store 1"}]}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	stores, err := client.SearchStoresByZIP(context.Background(), "98005")
	if err != nil {
		t.Fatalf("search stores by zip: %v", err)
	}

	if stores == nil || len(stores) != 1 {
		t.Fatalf("unexpected stores result: %+v", stores)
	}
	if stores[0].Name != "Store 1" {
		t.Fatalf("unexpected store name: %q", stores[0].Name)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/stores" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("zip"); got != "98005" {
		t.Fatalf("unexpected zip query value: %q", got)
	}

	consumerID := capturedReq.Header.Get("WM_CONSUMER.ID")
	if consumerID != "consumer-id-123" {
		t.Fatalf("unexpected WM_CONSUMER.ID: %q", consumerID)
	}
	timestamp := capturedReq.Header.Get("WM_CONSUMER.INTIMESTAMP")
	if timestamp == "" {
		t.Fatal("missing WM_CONSUMER.INTIMESTAMP")
	}
	keyVersion := capturedReq.Header.Get("WM_SEC.KEY_VERSION")
	if keyVersion != "1" {
		t.Fatalf("unexpected WM_SEC.KEY_VERSION: %q", keyVersion)
	}
	if capturedReq.Header.Get("WM_QOS.CORRELATION_ID") == "" {
		t.Fatal("missing WM_QOS.CORRELATION_ID")
	}

	rawSigHeader := capturedReq.Header.Get("WM_SEC.AUTH_SIGNATURE")
	if rawSigHeader == "" {
		t.Fatal("missing WM_SEC.AUTH_SIGNATURE")
	}
	signature, err := base64.StdEncoding.DecodeString(rawSigHeader)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}

	payload := fmt.Sprintf("%s\n%s\n%s\n", consumerID, timestamp, keyVersion)
	digest := sha256.Sum256([]byte(payload))
	if err := rsa.VerifyPKCS1v15(&privateKey.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestTaxonomy_DeserializesResponse(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`{
			"categories": [
				{
					"id": "1334134",
					"name": "Arts Crafts & Sewing",
					"path": "Arts Crafts & Sewing",
					"children": [
						{
							"id": "1334134_5899871",
							"name": "Art Supplies",
							"path": "Arts Crafts & Sewing/Art Supplies",
							"children": [
								{
									"id": "1334134_5899871_4519281",
									"name": "Aprons",
									"path": "Arts Crafts & Sewing/Art Supplies/Aprons"
								}
							]
						}
					]
				}
			]
		}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	taxonomy, err := client.Taxonomy(context.Background())
	if err != nil {
		t.Fatalf("taxonomy: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/taxonomy" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}

	if len(taxonomy.Categories) != 1 {
		t.Fatalf("unexpected categories count: %d", len(taxonomy.Categories))
	}
	root := taxonomy.Categories[0]
	if root.ID != "1334134" {
		t.Fatalf("unexpected root id: %s", root.ID)
	}
	if len(root.Children) != 1 {
		t.Fatalf("unexpected child count: %d", len(root.Children))
	}
	leaf := root.Children[0].Children[0]
	if leaf.Name != "Aprons" {
		t.Fatalf("unexpected leaf name: %s", leaf.Name)
	}
}

func TestTaxonomy_StatusError(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Taxonomy(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTaxonomy_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Taxonomy(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse taxonomy response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseOpenSSHKeyFromEnv_FromBase64PEM(t *testing.T) {
	t.Parallel()

	privateKey, encodedKey := newBase64RSAPrivateKey(t)

	parsed, err := parseOpenSSHKeyFromEnv(encodedKey)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if parsed.N.Cmp(privateKey.N) != 0 {
		t.Fatal("parsed key does not match generated key")
	}
}

func TestSearchStoresByZIP_StatusError(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.SearchStoresByZIP(context.Background(), "98005")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetLocationByID_NotSupported(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    "https://example.com",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.GetLocationByID(context.Background(), "999")
	if err == nil {
		t.Fatal("expected not-supported error")
	}
	if !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientIsID(t *testing.T) {
	t.Parallel()

	client := &Client{}
	tests := []struct {
		id   string
		want bool
	}{
		{id: "walmart_1", want: true},
		{id: "walmart_12345", want: true},
		{id: "walmart_", want: false},
		{id: "walmart-1", want: false},
		{id: "12345", want: false},
		{id: "walmart_12x", want: false},
	}

	for _, tc := range tests {
		if got := client.IsID(tc.id); got != tc.want {
			t.Fatalf("IsID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func newBase64RSAPrivateKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})
	encoded := base64.StdEncoding.EncodeToString(pemBytes)
	return privateKey, encoded
}
