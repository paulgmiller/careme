package brightdata

import (
	"context"
	"net/http"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
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

	retryTransport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper, got %T", client.Transport)
	}

	transport, ok := retryTransport.Client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected proxy *http.Transport, got %T", retryTransport.Client.HTTPClient.Transport)
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
	retryTransport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper when proxy disabled, got %T", client.Transport)
	}
	if retryTransport.Client.HTTPClient.Transport != http.DefaultTransport {
		t.Fatalf("expected default base transport, got %T", retryTransport.Client.HTTPClient.Transport)
	}
}

func TestNewProxySessionHTTPClient_UsesFreshSessionWithoutNestedRetries(t *testing.T) {
	t.Parallel()

	client, err := NewProxySessionHTTPClient(ProxyConfig{
		Host:     "brd.superproxy.io",
		Port:     "33335",
		Username: "user-name",
		Password: "secret-pass",
	}, "attempt2")
	if err != nil {
		t.Fatalf("NewProxySessionHTTPClient() error = %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected direct *http.Transport without nested retries, got %T", client.Transport)
	}
	proxyURL, err := transport.Proxy(mustRequest(t, http.MethodGet))
	if err != nil {
		t.Fatalf("transport.Proxy() error = %v", err)
	}
	if got, want := proxyURL.User.Username(), "user-name-session-attempt2"; got != want {
		t.Fatalf("unexpected proxy username: got %q want %q", got, want)
	}
}

func TestNewProxySessionHTTPClient_RejectsInvalidSessionID(t *testing.T) {
	t.Parallel()

	_, err := NewProxySessionHTTPClient(ProxyConfig{}, "attempt-2")
	if err == nil {
		t.Fatal("expected invalid session ID error")
	}
}

func TestWithRetries_OnlyRetriesGet5xx(t *testing.T) {
	t.Parallel()

	retryClient := withRetries(&http.Client{})

	transport, ok := retryClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper, got %T", retryClient.Transport)
	}

	tests := []struct {
		name   string
		method string
		status int
		want   bool
	}{
		{name: "get 502", method: http.MethodGet, status: http.StatusBadGateway, want: true},
		{name: "head 500", method: http.MethodHead, status: http.StatusInternalServerError, want: true},
		{name: "get 404", method: http.MethodGet, status: http.StatusNotFound, want: false},
		{name: "post 500", method: http.MethodPost, status: http.StatusInternalServerError, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tt.method, "https://example.com", nil)
			if err != nil {
				t.Fatalf("NewRequestWithContext() error = %v", err)
			}
			resp := &http.Response{StatusCode: tt.status, Request: req}

			got, err := transport.Client.CheckRetry(context.Background(), resp, nil)
			if err != nil {
				t.Fatalf("CheckRetry() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected retry decision: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestWithRetries_RespectsCanceledContext(t *testing.T) {
	t.Parallel()

	retryClient := withRetries(&http.Client{})
	transport, ok := retryClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper, got %T", retryClient.Transport)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := transport.Client.CheckRetry(ctx, &http.Response{
		StatusCode: http.StatusBadGateway,
		Request:    mustRequest(t, http.MethodGet),
	}, nil)
	if got {
		t.Fatal("expected canceled context not to retry")
	}
	if err != context.Canceled {
		t.Fatalf("unexpected error: got %v want %v", err, context.Canceled)
	}
}

func TestWithRetries_UsesLibraryDefaults(t *testing.T) {
	t.Parallel()

	retryClient := withRetries(&http.Client{})
	transport, ok := retryClient.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper, got %T", retryClient.Transport)
	}

	if got, want := transport.Client.RetryMax, 4; got != want {
		t.Fatalf("unexpected RetryMax: got %d want %d", got, want)
	}
	if got, want := transport.Client.RetryWaitMin, time.Second; got != want {
		t.Fatalf("unexpected RetryWaitMin: got %s want %s", got, want)
	}
	if got, want := transport.Client.RetryWaitMax, 30*time.Second; got != want {
		t.Fatalf("unexpected RetryWaitMax: got %s want %s", got, want)
	}
	if got := transport.Client.Backoff(time.Second, 30*time.Second, 0, nil); got != time.Second {
		t.Fatalf("unexpected default backoff for attempt 0: got %s want %s", got, time.Second)
	}
}

func mustRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	return req
}
