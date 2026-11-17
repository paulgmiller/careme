package brightdata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyConfigValidate_AllowsDisabled(t *testing.T) {
	t.Parallel()

	if (ProxyConfig{}).enabled() {
		t.Fatalf("Empty config should not be enabled")
	}
}

func TestProxyConfigValidate_RejectsPartialConfig(t *testing.T) {
	t.Parallel()

	enabled := (ProxyConfig{
		Host: "brd.superproxy.io",
		Port: "33335",
	}).enabled()
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

func TestNewProxyAwareHTTPClient_UsesRequestProxySession(t *testing.T) {
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

	retryTransport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper, got %T", client.Transport)
	}
	transport, ok := retryTransport.Client.HTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected proxy *http.Transport, got %T", retryTransport.Client.HTTPClient.Transport)
	}

	ctx := WithProxySessionID(context.Background(), "category123")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.example.com/products", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy() error = %v", err)
	}
	if got, want := proxyURL.String(), "http://user-name-session-category123:secret-pass@brd.superproxy.io:33335"; got != want {
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
	retryTransport, ok := client.Transport.(*retryablehttp.RoundTripper)
	if !ok {
		t.Fatalf("expected *retryablehttp.RoundTripper when proxy disabled, got %T", client.Transport)
	}
	if retryTransport.Client.HTTPClient.Transport != http.DefaultTransport {
		t.Fatalf("expected default base transport, got %T", retryTransport.Client.HTTPClient.Transport)
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

func TestWithRetries_ProxySessions(t *testing.T) {
	t.Parallel()
	for _, sticky := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
			rotate bool
		}{
			{name: "timeout", err: context.DeadlineExceeded, rotate: true},
			{name: "network timeout", err: &net.DNSError{Err: "timeout", IsTimeout: true}, rotate: true},
			{name: "server error", status: http.StatusBadGateway},
			{name: "network error", err: errors.New("connection reset")},
		} {
			t.Run(fmt.Sprintf("%s/sticky=%v", tc.name, sticky), func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				if sticky {
					ctx = WithProxySessionID(ctx, "original")
				}
				var sessions []string
				client := withRetries(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					session, ok := ProxySessionIDFromContext(req.Context())
					require.Equal(t, sticky, ok)
					if sticky {
						require.NotEmpty(t, session)
					}
					sessions = append(sessions, session)
					if len(sessions) == 1 && tc.err != nil {
						return nil, tc.err
					}
					status := http.StatusOK
					if len(sessions) == 1 {
						status = tc.status
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
				})})
				retries := client.Transport.(*retryablehttp.RoundTripper).Client
				retries.RetryWaitMin = 0
				retries.RetryWaitMax = 0
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Len(t, sessions, 2)
				assert.Equal(t, sticky && tc.rotate, sessions[0] != sessions[1])
				if sticky {
					assert.Equal(t, "original", sessions[0])
					// A later request using the category's context retains the
					// replacement session after the timeout was recovered.
					resp, err = client.Do(req.Clone(ctx))
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					require.Len(t, sessions, 3)
					assert.Equal(t, sessions[1], sessions[2])
				}
			})
		}
	}
}

func TestWithRetries_AttemptTimeoutRotatesSession(t *testing.T) {
	t.Parallel()
	var sessions []string
	client := withRetries(&http.Client{Timeout: time.Millisecond, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		session, ok := ProxySessionIDFromContext(req.Context())
		require.True(t, ok)
		sessions = append(sessions, session)
		if len(sessions) == 1 {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	})})
	retries := client.Transport.(*retryablehttp.RoundTripper).Client
	retries.RetryWaitMin = 0
	retries.RetryWaitMax = 0
	req, err := http.NewRequestWithContext(WithProxySessionID(t.Context(), "original"), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, sessions, 2)
	assert.NotEqual(t, sessions[0], sessions[1])
}

func TestWithRetries_ConcurrentRequestsCanRotateSharedSession(t *testing.T) {
	t.Parallel()
	ctx := WithProxySessionID(t.Context(), "original")
	client := withRetries(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		session, ok := ProxySessionIDFromContext(req.Context())
		require.True(t, ok)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(session)), Request: req}, nil
	})})
	retries := client.Transport.(*retryablehttp.RoundTripper).Client
	sessions := make(chan string, 8)
	t.Run("requests", func(t *testing.T) {
		for i := range cap(sessions) {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()
				_, _ = retries.CheckRetry(ctx, nil, context.DeadlineExceeded)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				sessions <- string(body)
			})
		}
	})
	close(sessions)
	count := 0
	for session := range sessions {
		assert.NotEmpty(t, session)
		assert.NotEqual(t, "original", session)
		count++
	}
	assert.Equal(t, 8, count)
}

func TestWithRetries_CheckRetryRotatesBeforeReturning(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		rotate bool
	}{
		{name: "timeout", err: context.DeadlineExceeded, rotate: true},
		{name: "canceled", err: context.Canceled},
		{name: "server error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := WithProxySessionID(t.Context(), "original")
			client := withRetries(&http.Client{})
			_, _ = client.Transport.(*retryablehttp.RoundTripper).Client.CheckRetry(ctx, nil, tc.err)
			session, ok := ProxySessionIDFromContext(ctx)
			require.True(t, ok)
			assert.Equal(t, tc.rotate, session != "original")
		})
	}
}
