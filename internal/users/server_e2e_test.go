package users

import (
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	srv := NewHandler(storage, nil, cacheStore, auth.DefaultMock())
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

func TestUserPastRecipes_E2E_ShowsInlineCookedWidgetForHashedRecipes(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)

	user := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"user@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{
			{Title: "Braised Chicken", Hash: "hash-cooked==", CreatedAt: time.Now()},
			{Title: "Market Pasta", Hash: "hash-open", CreatedAt: time.Now().Add(-time.Hour)},
			{Title: "Notebook Soup", CreatedAt: time.Now().Add(-2 * time.Hour)},
		},
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	if err := cacheStore.Put(context.Background(), "recipe_feedback/hash-cooked==", `{"cooked":true,"stars":4,"comment":"Great flavor."}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to seed cooked feedback: %v", err)
	}

	srv := NewHandler(storage, nil, cacheStore, auth.DefaultMock())
	mux := http.NewServeMux()
	srv.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/user?tab=past", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d for GET /user?tab=past, got %d", http.StatusOK, resp.Code)
	}

	body := resp.Body.String()
	if !strings.Contains(body, `hx-post="/recipe/hash-cooked==/feedback"`) {
		t.Fatalf("expected hashed recipe feedback endpoint in body: %s", body)
	}
	if !strings.Contains(body, "Update feedback") {
		t.Fatalf("expected cooked recipe to show update state, got body: %s", body)
	}
	if !strings.Contains(body, `value="4"`) {
		t.Fatalf("expected saved star rating in body: %s", body)
	}
	if !strings.Contains(body, "Great flavor.") {
		t.Fatalf("expected saved comment in body: %s", body)
	}
	if !strings.Contains(body, `hx-post="/recipe/hash-open/feedback"`) {
		t.Fatalf("expected uncooked hashed recipe widget in body: %s", body)
	}
	if !strings.Contains(body, `id="saved-recipe-feedback-hash-cooked-button"`) || !strings.Contains(body, `id="saved-recipe-feedback-hash-open-button"`) {
		t.Fatalf("expected unique widget ids for saved recipes, got body: %s", body)
	}
	if strings.Contains(body, `id="saved-recipe-feedback-hash-cooked==-button"`) {
		t.Fatalf("expected padded hash characters to be stripped from widget ids, got body: %s", body)
	}
	if !strings.Contains(body, "I cooked it!") {
		t.Fatalf("expected uncooked widget label in body: %s", body)
	}
	if strings.Contains(body, `hx-post="/recipe//feedback"`) {
		t.Fatalf("did not expect feedback widget for manual recipe entry: %s", body)
	}
	if !strings.Contains(body, `/static/htmx@2.0.8.js`) {
		t.Fatalf("expected htmx script on user page, got body: %s", body)
	}
}
