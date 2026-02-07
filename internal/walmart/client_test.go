package walmart

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`{"results":[{"id":"1","name":"Store 1"}]}`))
	}))
	t.Cleanup(server.Close)

	keyPath := writePKCS1Key(t, privateKey)
	client, err := NewClient(Config{
		ConsumerID:     "consumer-id-123",
		KeyVersion:     "1",
		PrivateKeyPath: keyPath,
		BaseURL:        server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	body, err := client.SearchStoresByZIP(context.Background(), "98005")
	if err != nil {
		t.Fatalf("search stores by zip: %v", err)
	}
	if !json.Valid(body) {
		t.Fatalf("expected valid JSON, got: %s", string(body))
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

func TestSearchStores_UsesZipOnly(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`[{"id":"1","name":"Store 1"}]`))
	}))
	t.Cleanup(server.Close)

	keyPath := writePKCS1Key(t, privateKey)
	client, err := NewClient(Config{
		ConsumerID:     "consumer-id-123",
		KeyVersion:     "1",
		PrivateKeyPath: keyPath,
		BaseURL:        server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	body, err := client.SearchStores(context.Background(), StoresQuery{
		Lat:  "47.610149", // ignored in ZIP-only mode
		Lon:  "-122.201515",
		Zip:  "98005",
		City: "Bellevue",
	})
	if err != nil {
		t.Fatalf("search stores: %v", err)
	}
	if !json.Valid(body) {
		t.Fatalf("expected valid JSON, got: %s", string(body))
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	q := capturedReq.URL.Query()
	if got := q.Get("zip"); got != "98005" {
		t.Fatalf("unexpected zip query value: %q", got)
	}
	if got := q.Get("lat"); got != "" {
		t.Fatalf("unexpected lat query value: %q", got)
	}
	if got := q.Get("lon"); got != "" {
		t.Fatalf("unexpected lon query value: %q", got)
	}
	if got := q.Get("city"); got != "" {
		t.Fatalf("unexpected city query value: %q", got)
	}
}

func TestStoresBaseURLForZIP_ConvertsProductV2Path(t *testing.T) {
	t.Parallel()

	got := storesBaseURLForZIP("https://developer.api.walmart.com/api-proxy/service/affil/product/v2")
	want := "https://developer.api.walmart.com/api-proxy/service/affil/v2"
	if got != want {
		t.Fatalf("unexpected ZIP base URL conversion: got %q want %q", got, want)
	}

	same := storesBaseURLForZIP("http://127.0.0.1:8080")
	if same != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected non-matching URL conversion: %q", same)
	}
}

func TestSearchStores_MissingZipReturnsError(t *testing.T) {
	t.Parallel()

	client := &Client{}
	_, err := client.SearchStores(context.Background(), StoresQuery{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "zip code is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRSAPrivateKey_FromPKCS1PEM(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	keyPath := writePKCS1Key(t, privateKey)

	loaded, err := LoadRSAPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	if loaded.N.Cmp(privateKey.N) != 0 {
		t.Fatal("loaded key does not match generated key")
	}
}

func TestSearchStoresByZIP_StatusError(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	keyPath := writePKCS1Key(t, privateKey)
	client, err := NewClient(Config{
		ConsumerID:     "consumer-id-123",
		KeyVersion:     "1",
		PrivateKeyPath: keyPath,
		BaseURL:        server.URL,
		HTTPClient:     server.Client(),
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

func writePKCS1Key(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()

	// Use PKCS8 encoding for portability with current Go versions.
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatalf("encode key: %v", err)
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "key.pem")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}
