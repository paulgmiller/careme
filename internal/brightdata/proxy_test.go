package brightdata

import (
	"net/http"
	"testing"

	"careme/internal/httpx"
)

func TestProxyConfigValidate_AllowsDisabled(t *testing.T) {
	t.Parallel()

	if (ProxyConfig{}).Enabled() {
		t.Fatalf("Empty config should not be enabled")
	}
}

func TestProxyConfigValidate_RejectsPartialConfig(t *testing.T) {
	t.Parallel()

	enabled := (ProxyConfig{
		Host: "brd.superproxy.io",
		Port: "33335",
	}).Enabled()
	if enabled {
		t.Fatal("expected diabled when only host and port provided")
	}
}

func TestProxyConfigProxyURL_BuildsProxyURL(t *testing.T) {
	t.Parallel()

	proxyURL := (ProxyConfig{
		Host:     "brd.superproxy.io",
		Port:     "33335",
		Username: "user-name",
		Password: "secret-pass",
	}).proxyURL()

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

	retryTransport, ok := client.Transport.(*httpx.RetryTransport)
	if !ok {
		t.Fatalf("expected *httpx.RetryTransport, got %T", client.Transport)
	}

	transport, ok := retryTransport.Base.(*http.Transport)
	if !ok {
		t.Fatalf("expected wrapped base *http.Transport, got %T", retryTransport.Base)
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
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("expected embedded BrightData CA pool to be configured")
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
	retryTransport, ok := client.Transport.(*httpx.RetryTransport)
	if !ok {
		t.Fatalf("expected *httpx.RetryTransport when proxy disabled, got %T", client.Transport)
	}
	if retryTransport.Base != http.DefaultTransport {
		t.Fatalf("expected wrapped default transport, got %T", retryTransport.Base)
	}
}
