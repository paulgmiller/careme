package watchdog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubWatchdog struct {
	calls int
	err   error
}

func (s *stubWatchdog) Watchdog(context.Context) error {
	s.calls++
	return s.err
}

func TestServerRegisterWatchdog(t *testing.T) {
	t.Parallel()

	dog := &stubWatchdog{}
	server := &Server{}
	server.Add("staples", dog, 6*time.Hour)
	mux := http.NewServeMux()
	server.Register(mux)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/watchdogs/staples", nil))
	if got, want := first.Code, http.StatusOK; got != want {
		t.Fatalf("first status = %d, want %d", got, want)
	}
	if got, want := dog.calls, 1; got != want {
		t.Fatalf("watchdog calls after first request = %d, want %d", got, want)
	}

	second := httptest.NewRecorder()
	mux.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/watchdogs/staples", nil))
	if got, want := second.Code, http.StatusTooManyRequests; got != want {
		t.Fatalf("second status = %d, want %d", got, want)
	}
	if !strings.Contains(second.Body.String(), "can only call watchdog every "+(6*time.Hour).String()) {
		t.Fatalf("second body = %q, want rate limit message", second.Body.String())
	}
	if got, want := dog.calls, 1; got != want {
		t.Fatalf("watchdog calls after second request = %d, want %d", got, want)
	}
}

func TestServerRegisterWatchdogError(t *testing.T) {
	t.Parallel()

	dog := &stubWatchdog{err: errors.New("boom")}
	server := &Server{}
	server.Add("produce", dog, 30*time.Minute)
	mux := http.NewServeMux()
	server.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/watchdogs/produce", nil))
	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if !strings.Contains(rec.Body.String(), "produce not ready: boom") {
		t.Fatalf("body = %q, want watchdog error", rec.Body.String())
	}
	if got, want := dog.calls, 1; got != want {
		t.Fatalf("watchdog calls = %d, want %d", got, want)
	}
}
