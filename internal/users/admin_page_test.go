package users

import (
	"careme/internal/cache"
	utypes "careme/internal/users/types"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestAdminUsersPageRendersEmailsAndRecipes(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)
	now := time.Now()

	if err := storage.Update(&utypes.User{
		ID:          "user_1",
		Email:       []string{"alice@example.com"},
		ShoppingDay: time.Monday.String(),
		LastRecipes: []utypes.Recipe{
			{Title: "Tomato Soup", CreatedAt: now},
			{Title: "Veggie Tacos", CreatedAt: now.Add(-1 * time.Hour)},
		},
	}); err != nil {
		t.Fatalf("update user_1: %v", err)
	}

	if err := storage.Update(&utypes.User{
		ID:          "user_2",
		Email:       []string{"bob@example.com", "bobby@example.com"},
		ShoppingDay: time.Tuesday.String(),
	}); err != nil {
		t.Fatalf("update user_2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rr := httptest.NewRecorder()

	AdminUsersPage(storage).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want text/html", got)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"alice@example.com",
		"bob@example.com",
		"bobby@example.com",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response body missing %q: %s", want, body)
		}
	}
	if !regexp.MustCompile(`<td>\s*2\s*</td>`).MatchString(body) {
		t.Fatalf("response body missing saved recipe count 2: %s", body)
	}
	if !regexp.MustCompile(`<td>\s*0\s*</td>`).MatchString(body) {
		t.Fatalf("response body missing saved recipe count 0: %s", body)
	}
	for _, unwanted := range []string{"Tomato Soup", "Veggie Tacos"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("response body should not include recipe title %q: %s", unwanted, body)
		}
	}
}

func TestAdminUsersPageMethodNotAllowed(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	req := httptest.NewRequest(http.MethodPost, "/users", nil)
	rr := httptest.NewRecorder()

	AdminUsersPage(storage).ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminUsersPageFormatEmails(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	if err := storage.Update(&utypes.User{
		ID:          "user_1",
		Email:       []string{"alice@example.com", "Bob@example.com"},
		ShoppingDay: time.Wednesday.String(),
	}); err != nil {
		t.Fatalf("update user_1: %v", err)
	}
	if err := storage.Update(&utypes.User{
		ID:          "user_2",
		Email:       []string{" bob@example.com ", "charlie@example.com"},
		ShoppingDay: time.Thursday.String(),
	}); err != nil {
		t.Fatalf("update user_2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users?format=emails", nil)
	rr := httptest.NewRecorder()

	AdminUsersPage(storage).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", got)
	}

	want := "alice@example.com\nbob@example.com\ncharlie@example.com\n"
	if rr.Body.String() != want {
		t.Fatalf("body = %q, want %q", rr.Body.String(), want)
	}
}
