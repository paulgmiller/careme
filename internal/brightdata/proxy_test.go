package brightdata

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

func TestNewSessionID(t *testing.T) {
	t.Parallel()

	first, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID returned error: %v", err)
	}
	second, err := newSessionID()
	if err != nil {
		t.Fatalf("newSessionID returned error: %v", err)
	}
	if first == second {
		t.Fatalf("expected unique session IDs, got %q twice", first)
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(first) {
		t.Fatalf("unexpected session ID format: %q", first)
	}
}

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

	sessionTransport, ok := client.Transport.(proxySessionRoundTripper)
	if !ok {
		t.Fatalf("expected proxySessionRoundTripper, got %T", client.Transport)
	}
	retryTransport, ok := sessionTransport.next.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected nested *retryablehttp.RoundTripper, got %T", sessionTransport.next)
	}

	transport, ok := retryTransport.Client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected proxy *http.Transport, got %T", retryTransport.Client.HTTPClient.Transport)
	}

	req, err := http.NewRequest(http.MethodGet, "https://www.example.com/products", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	session := &proxySession{id: "attempt1"}
	req = req.WithContext(context.WithValue(req.Context(), proxySessionContextKey{}, session))
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy() error = %v", err)
	}
	if proxyURL == nil {
		t.Fatal("expected proxy URL")
	}
	if got, want := proxyURL.String(), "http://user-name-session-attempt1:secret-pass@brd.superproxy.io:33335"; got != want {
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

func TestRetriable_RotatesProxySessionAfterTimeout(t *testing.T) {
	t.Parallel()

	session := &proxySession{id: "beforetimeout"}
	ctx := context.WithValue(context.Background(), proxySessionContextKey{}, session)

	retry, err := retriable(ctx, nil, proxyTimeoutError{})

	if !retry {
		t.Fatal("expected timeout to be retried")
	}
	if err == nil {
		t.Fatal("expected timeout error to be preserved")
	}
	if got := session.ID(); got == "beforetimeout" {
		t.Fatalf("expected timeout to rotate session, still got %q", got)
	}
}

func TestRetriable_KeepsProxySessionAfterNonTimeoutError(t *testing.T) {
	t.Parallel()

	session := &proxySession{id: "keepme"}
	ctx := context.WithValue(context.Background(), proxySessionContextKey{}, session)

	retry, err := retriable(ctx, nil, errors.New("connection reset"))

	if !retry || err == nil {
		t.Fatalf("expected network error to be retried and preserved, got retry=%t error=%v", retry, err)
	}
	if got, want := session.ID(), "keepme"; got != want {
		t.Fatalf("unexpected session rotation: got %q want %q", got, want)
	}
}

func TestProxySessionRoundTripper_StartsEachRequestWithSession(t *testing.T) {
	t.Parallel()

	var sessionID string
	transport := proxySessionRoundTripper{next: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		session, ok := req.Context().Value(proxySessionContextKey{}).(*proxySession)
		if !ok {
			t.Fatal("request context missing proxy session")
		}
		sessionID = session.ID()
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})}

	_, err := transport.RoundTrip(mustRequest(t, http.MethodGet))
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(sessionID) {
		t.Fatalf("unexpected session ID format: %q", sessionID)
	}
}

type proxyTimeoutError struct{}

func (proxyTimeoutError) Error() string   { return "proxy timeout" }
func (proxyTimeoutError) Timeout() bool   { return true }
func (proxyTimeoutError) Temporary() bool { return true }

type proxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
