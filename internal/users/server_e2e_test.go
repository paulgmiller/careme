package users

import (
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	initTemplatesOnce sync.Once
	initTemplatesErr  error
)

func TestMain(m *testing.M) {
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestUserPageUpdate_E2E(t *testing.T) {

	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)

	srv := NewHandler(storage, nil, auth.DefaultMock())
	mux := http.NewServeMux()
	srv.Register(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/user?tab=customize", nil)
	getResp := httptest.NewRecorder()
	mux.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("expected status %d for GET /user, got %d", http.StatusOK, getResp.Code)
	}
	if body := getResp.Body.String(); !strings.Contains(body, `name="directive"`) {
		t.Fatalf("expected generation prompt field on user page, got body: %s", body)
	}

	form := url.Values{
		"favorite_store": {"70500874"},
		"shopping_day":   {"Monday"},
		"mail_opt_in":    {"1"},
		"directive":      {"Generate 4 recipes. No shellfish."},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/user", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResp := httptest.NewRecorder()
	mux.ServeHTTP(postResp, postReq)

	if postResp.Code != http.StatusOK {
		t.Fatalf("expected status %d for POST /user, got %d", http.StatusOK, postResp.Code)
	}
	postBody := postResp.Body.String()
	if !strings.Contains(postBody, "Settings saved successfully!") {
		t.Fatalf("expected success message in response body, got body: %s", postBody)
	}
	if !strings.Contains(postBody, "Generate 4 recipes. No shellfish.") {
		t.Fatalf("expected prompt text in rendered page, got body: %s", postBody)
	}

	user, err := storage.GetByID("mock-clerk-user-id")
	if err != nil {
		t.Fatalf("expected saved user, got error: %v", err)
	}
	if got, want := user.FavoriteStore, "70500874"; got != want {
		t.Fatalf("expected favorite_store %q, got %q", want, got)
	}
	if got, want := user.ShoppingDay, "Monday"; got != want {
		t.Fatalf("expected shopping_day %q, got %q", want, got)
	}
	if !user.MailOptIn {
		t.Fatal("expected mail_opt_in to be true")
	}
	if got, want := user.Directive, "Generate 4 recipes. No shellfish."; got != want {
		t.Fatalf("expected directive %q, got %q", want, got)
	}
}
