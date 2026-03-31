package httpx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRetryTransport_RetriesGetOn5xxUntilSuccess(t *testing.T) {
	t.Parallel()

	attempts := 0
	sleepCalls := 0
	client := WrapClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			status := http.StatusBadGateway
			if attempts == 3 {
				status = http.StatusOK
			}
			return response(status), nil
		}),
	}, RetryConfig{
		Sleep: func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
		RandFloat64: func() float64 { return 0 },
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := attempts, 3; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
	if got, want := sleepCalls, 2; got != want {
		t.Fatalf("unexpected sleep calls: got %d want %d", got, want)
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("unexpected status: got %d want %d", got, want)
	}
}

func TestRetryTransport_StopsAfterMaxRetries(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := WrapClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			return response(http.StatusServiceUnavailable), nil
		}),
	}, RetryConfig{
		MaxRetries: 2,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := attempts, 3; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
	if got, want := resp.StatusCode, http.StatusServiceUnavailable; got != want {
		t.Fatalf("unexpected status: got %d want %d", got, want)
	}
}

func TestRetryTransport_DoesNotRetryOn4xx(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := WrapClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			return response(http.StatusNotFound), nil
		}),
	}, RetryConfig{
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("unexpected sleep call")
			return nil
		},
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := attempts, 1; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
}

func TestRetryTransport_DoesNotRetryNonIdempotentMethod(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := WrapClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			return response(http.StatusInternalServerError), nil
		}),
	}, RetryConfig{
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("unexpected sleep call")
			return nil
		},
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := attempts, 1; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
}

func TestRetryTransport_RespectsCanceledContext(t *testing.T) {
	t.Parallel()

	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := WrapClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			return response(http.StatusBadGateway), nil
		}),
	}, RetryConfig{
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("unexpected sleep call")
			return nil
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := attempts, 1; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(http.StatusText(status))),
	}
}
