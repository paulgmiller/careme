package brightdata

import (
	"net/http"
	"testing"
)

func TestProxyConfigValidate_AllowsDisabled(t *testing.T) {
	t.Parallel()

	if err := (ProxyConfig{}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProxyConfigValidate_RejectsPartialConfig(t *testing.T) {
	t.Parallel()

	err := (ProxyConfig{
		Host: "brd.superproxy.io",
		Port: "33335",
	}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestProxyConfigProxyURL_BuildsProxyURL(t *testing.T) {
	t.Parallel()

	proxyURL, err := (ProxyConfig{
		Host:     "brd.superproxy.io",
		Port:     "33335",
		Username: "user-name",
		Password: "secret-pass",
	}).ProxyURL()
	if err != nil {
		t.Fatalf("ProxyURL() error = %v", err)
	}

	if got, want := proxyURL.String(), "http://user-name:secret-pass@brd.superproxy.io:33335"; got != want {
		t.Fatalf("unexpected proxy URL: got %q want %q", got, want)
	}
}

func TestNewProxyAwareHTTPClient_UsesConfiguredProxy(t *testing.T) {
	t.Parallel()

	client, err := NewProxyAwareHTTPClient(ProxyConfig{
		Host:     "brd.superproxy.io",
		Port:     "33335",
		Username: "user-name",
		Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("NewProxyAwareHTTPClient() error = %v", err)
	}

	if client.Timeout != 0 {
		t.Fatalf("expected no client timeout, got %s", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}

	req, err := http.NewRequest(http.MethodGet, "https://www.example.com/products", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy() error = %v", err)
	}
	if proxyURL == nil {
		t.Fatal("expected proxy URL")
	}
	if got, want := proxyURL.String(), "http://user-name:secret-pass@brd.superproxy.io:33335"; got != want {
		t.Fatalf("unexpected proxy URL: got %q want %q", got, want)
	}
}

func TestNewProxyAwareHTTPClient_DisabledLeavesDefaultTransport(t *testing.T) {
	t.Parallel()

	client, err := NewProxyAwareHTTPClient(ProxyConfig{})
	if err != nil {
		t.Fatalf("NewProxyAwareHTTPClient() error = %v", err)
	}
	if client.Timeout != 0 {
		t.Fatalf("expected no client timeout, got %s", client.Timeout)
	}
	if client.Transport != nil {
		t.Fatalf("expected nil transport when proxy disabled, got %T", client.Transport)
	}
}
