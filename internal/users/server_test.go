package users

import (
	"careme/internal/cache"
	utypes "careme/internal/users/types"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testAuthClient struct{}

func (t testAuthClient) GetUserEmail(_ context.Context, _ string) (string, error) {
	return "user@example.com", nil
}

func (t testAuthClient) GetUserIDFromRequest(_ *http.Request) (string, error) {
	return "user-1", nil
}

func (t testAuthClient) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (t testAuthClient) Register(_ *http.ServeMux) {}

func TestHandleUser_SavesDirective(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: template.Must(template.New("user").Parse("ok")),
		clerk:    testAuthClient{},
	}

	form := url.Values{
		"directive": {"Generate 5 recipes for 4 people."},
	}
	req := httptest.NewRequest(http.MethodPost, "/user", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handleUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	user, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("expected user to be stored, got error %v", err)
	}
	if got, want := user.Directive, "Generate 5 recipes for 4 people."; got != want {
		t.Fatalf("expected directive %q, got %q", want, got)
	}
}

func TestHandleUser_ClearsDirective(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: template.Must(template.New("user").Parse("ok")),
		clerk:    testAuthClient{},
	}

	existing := &utypes.User{
		ID:          "user-1",
		Email:       []string{"user@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		Directive:   "Old prompt",
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	form := url.Values{
		"directive": {""},
	}
	req := httptest.NewRequest(http.MethodPost, "/user", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handleUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	user, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("expected user to be stored, got error %v", err)
	}
	if user.Directive != "" {
		t.Fatalf("expected generation prompt to be cleared, got %q", user.Directive)
	}
}
