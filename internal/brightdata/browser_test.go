package brightdata

import (
	"encoding/base64"
	"net/url"
	"testing"
)

func TestBrowserWSEndpointUsesCredentialsFromURL(t *testing.T) {
	t.Parallel()

	wsEndpoint, authHeader, err := browserWSEndpoint("wss://user:pass@brd.superproxy.io:9222", "")
	if err != nil {
		t.Fatalf("browserWSEndpoint returned error: %v", err)
	}

	if wsEndpoint != "wss://brd.superproxy.io:9222/" {
		t.Fatalf("unexpected endpoint: %q", wsEndpoint)
	}

	wantHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if authHeader != wantHeader {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}
}

func TestBrowserWSEndpointUsesExplicitAuthFallback(t *testing.T) {
	t.Parallel()

	wsEndpoint, authHeader, err := browserWSEndpoint("wss://brd.superproxy.io:9222", "user:pass")
	if err != nil {
		t.Fatalf("browserWSEndpoint returned error: %v", err)
	}

	if wsEndpoint != "wss://brd.superproxy.io:9222/" {
		t.Fatalf("unexpected endpoint: %q", wsEndpoint)
	}

	wantHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if authHeader != wantHeader {
		t.Fatalf("unexpected auth header: %q", authHeader)
	}
}

func TestBrowserAuthHeaderRejectsMissingPassword(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("wss://brd.superproxy.io:9222")
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}

	if _, err := browserAuthHeader(parsed, "useronly"); err == nil {
		t.Fatal("expected error for invalid auth format")
	}
}
