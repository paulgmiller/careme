package users

import (
	"careme/internal/auth"
	"careme/internal/cache"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

type noSessionAuth struct{}

func (n noSessionAuth) GetUserEmail(_ context.Context, _ string) (string, error) {
	return "", auth.ErrNoSession
}

func (n noSessionAuth) GetUserIDFromRequest(_ *http.Request) (string, error) {
	return "", auth.ErrNoSession
}

func (n noSessionAuth) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (n noSessionAuth) Register(_ *http.ServeMux) {}

func newFavoriteTestServer(t *testing.T, clerk auth.AuthClient) *server {
	t.Helper()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	return &server{
		storage: NewStorage(cacheStore),
		clerk:   clerk,
	}
}

func TestHandleFavoriteHTMXRefreshesPage(t *testing.T) {
	t.Parallel()
	s := newFavoriteTestServer(t, auth.DefaultMock())

	form := url.Values{
		"favorite_store": {"222"},
	}
	req := httptest.NewRequest(http.MethodPost, "/user/favorite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	s.handleFavorite(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if got := rr.Header().Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected HX-Refresh true, got %q", got)
	}

	user, err := s.storage.GetByID("mock-clerk-user-id")
	if err != nil {
		t.Fatalf("expected user to be stored, got error %v", err)
	}
	if user.FavoriteStore != "222" {
		t.Fatalf("expected favorite store to be 222, got %q", user.FavoriteStore)
	}
}

func TestHandleFavoriteRejectsNonHTMXRequest(t *testing.T) {
	t.Parallel()
	s := newFavoriteTestServer(t, auth.DefaultMock())

	form := url.Values{
		"favorite_store": {"444"},
	}
	req := httptest.NewRequest(http.MethodPost, "/user/favorite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handleFavorite(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleFavoriteNoSessionHTMXSetsRedirectHeader(t *testing.T) {
	t.Parallel()
	s := newFavoriteTestServer(t, noSessionAuth{})

	form := url.Values{
		"favorite_store": {"555"},
	}
	req := httptest.NewRequest(http.MethodPost, "/user/favorite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	s.handleFavorite(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/" {
		t.Fatalf("expected HX-Redirect to /, got %q", got)
	}
}
