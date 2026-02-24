package admin

import (
	"careme/internal/auth"
	"careme/internal/config"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubAuthClient struct {
	userID    string
	userIDErr error
	email     string
	emailErr  error
}

func (s stubAuthClient) GetUserEmail(_ context.Context, _ string) (string, error) {
	if s.emailErr != nil {
		return "", s.emailErr
	}
	return s.email, nil
}

func (s stubAuthClient) GetUserIDFromRequest(_ *http.Request) (string, error) {
	if s.userIDErr != nil {
		return "", s.userIDErr
	}
	return s.userID, nil
}

func (s stubAuthClient) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (s stubAuthClient) Register(_ *http.ServeMux) {}

func TestMiddlewareWrapRejectsNoSession(t *testing.T) {
	t.Parallel()

	m := New(&config.Config{
		Admin: config.AdminConfig{
			Emails: []string{"admin@example.com"},
		},
	}, stubAuthClient{userIDErr: auth.ErrNoSession})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Enforce(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMiddlewareWrapRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	m := New(&config.Config{
		Admin: config.AdminConfig{
			Emails: []string{"admin@example.com"},
		},
	}, stubAuthClient{
		userID: "user_123",
		email:  "user@example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Enforce(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMiddlewareWrapAllowsAdmin(t *testing.T) {
	t.Parallel()

	m := New(&config.Config{
		Admin: config.AdminConfig{
			Emails: []string{"admin@example.com"},
		},
	}, stubAuthClient{
		userID: "user_123",
		email:  "ADMIN@example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Enforce(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestMiddlewareWrapRejectsEmailLookupFailure(t *testing.T) {
	t.Parallel()

	m := New(&config.Config{
		Admin: config.AdminConfig{
			Emails: []string{"admin@example.com"},
		},
	}, stubAuthClient{
		userID:   "user_123",
		emailErr: errors.New("lookup failed"),
	})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Enforce(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
}

func TestMiddlewareWrapAllowsAnyLoggedInUserWhenNoAdminsConfigured(t *testing.T) {
	t.Parallel()

	m := New(&config.Config{}, stubAuthClient{
		userID: "user_123",
		email:  "user@example.com",
	})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	rr := httptest.NewRecorder()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	m.Enforce(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
}
