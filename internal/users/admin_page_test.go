package users

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	utypes "careme/internal/users/types"
)

func TestAdminUsersPageRendersEmailsAndRecipes(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)
	now := time.Now()

	if err := storage.Update(&utypes.User{
		ID:          "2b190b35-10c6-4d6f-9858-3dd73c4155a0",
		Email:       []string{"legacy@example.com"},
		CreatedAt:   now.AddDate(0, 0, -10),
		ShoppingDay: time.Monday.String(),
	}); err != nil {
		t.Fatalf("update legacy user: %v", err)
	}

	if err := storage.Update(&utypes.User{
		ID:          "user_1",
		Email:       []string{"alice@example.com"},
		CreatedAt:   time.Date(2026, time.March, 1, 15, 4, 5, 0, time.UTC),
		ShoppingDay: time.Monday.String(),
		LastRecipes: []utypes.Recipe{
			{Title: "Tomato Soup", Hash: "hash-1", CreatedAt: now},
			{Title: "Veggie Tacos", Hash: "hash-2", CreatedAt: now.Add(-1 * time.Hour)},
		},
	}); err != nil {
		t.Fatalf("update user_1: %v", err)
	}

	if err := storage.Update(&utypes.User{
		ID:          "user_2",
		Email:       []string{"bob@example.com", "bobby@example.com"},
		CreatedAt:   time.Date(2026, time.March, 2, 9, 30, 0, 0, time.UTC),
		ShoppingDay: time.Tuesday.String(),
		LastRecipes: []utypes.Recipe{
			{Title: "Pasta", Hash: "hash-3", CreatedAt: now},
		},
	}); err != nil {
		t.Fatalf("update user_2: %v", err)
	}

	if err := fc.Put(t.Context(), "recipe_feedback/hash-1", `{"cooked": true}`, cache.Unconditional()); err != nil {
		t.Fatalf("put feedback hash-1: %v", err)
	}
	if err := fc.Put(t.Context(), "recipe_feedback/hash-2", `{"cooked": false}`, cache.Unconditional()); err != nil {
		t.Fatalf("put feedback hash-2: %v", err)
	}
	if err := fc.Put(t.Context(), "recipe_feedback/hash-3", `{"cooked": true}`, cache.Unconditional()); err != nil {
		t.Fatalf("put feedback hash-3: %v", err)
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
	if strings.Contains(body, "legacy@example.com") {
		t.Fatalf("response body should not include legacy guid account: %s", body)
	}
	if !regexp.MustCompile(`<td>\s*<a href="/admin/users/user_1">user_1</a>\s*</td>[\s\S]*?<td>\s*2\s*</td>[\s\S]*?<td>\s*1\s*</td>`).MatchString(body) {
		t.Fatalf("response body missing user_1 row with saved/cooked counts: %s", body)
	}
	if !regexp.MustCompile(`<td>\s*<a href="/admin/users/user_2">user_2</a>\s*</td>[\s\S]*?<td>\s*1\s*</td>[\s\S]*?<td>\s*1\s*</td>`).MatchString(body) {
		t.Fatalf("response body missing user_2 row with saved/cooked counts: %s", body)
	}
	for _, want := range []string{"2026-03-01", "2026-03-02"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response body missing created date %q: %s", want, body)
		}
	}
	if strings.Contains(body, "0001-01-01") {
		t.Fatalf("response body should not include zero created date: %s", body)
	}
	if strings.Index(body, "user_1") > strings.Index(body, "user_2") {
		t.Fatalf("expected users sorted by saved recipe count descending: %s", body)
	}
	for _, unwanted := range []string{"Tomato Soup", "Veggie Tacos"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("response body should not include recipe title %q: %s", unwanted, body)
		}
	}
}

func TestAdminUserDetailPage(t *testing.T) {
	t.Parallel()
	storage := NewStorage(cache.NewFileCache(t.TempDir()))
	if err := storage.Update(&utypes.User{
		ID:            "user_1",
		Email:         []string{"alice@example.com"},
		ShoppingDay:   time.Monday.String(),
		Directive:     "Use <fresh> herbs\nNo shellfish",
		FavoriteStore: "wholefoods_123",
		LastRecipes: []utypes.Recipe{
			{Title: "Tomato & <Basil> Soup", Hash: "hash-1"},
			{Title: "Manual recipe"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /users/{id}", AdminUserDetailPage(storage))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/users/user_1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`href="/admin/users"`, "alice@example.com", "Use &lt;fresh&gt; herbs\nNo shellfish",
		`href="/recipe/hash-1"`, "Tomato &amp; &lt;Basil&gt; Soup", "Manual recipe", "Saved recipes (2)",
		"Favorite store", "<code>wholefoods_123</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<fresh>") || strings.Contains(body, "<Basil>") || strings.Contains(body, `href="/recipe/"`) {
		t.Fatalf("unescaped text or empty recipe link in body: %s", body)
	}
}

func TestAdminUserDetailPageEmptyAndMissing(t *testing.T) {
	t.Parallel()
	storage := NewStorage(cache.NewFileCache(t.TempDir()))
	if err := storage.Update(&utypes.User{ID: "user_empty", Email: []string{"empty@example.com"}, ShoppingDay: time.Monday.String()}); err != nil {
		t.Fatal(err)
	}
	handler := AdminUserDetailPage(storage)
	for _, tc := range []struct {
		name, id, method string
		status           int
		body             string
	}{
		{"empty", "user_empty", http.MethodGet, http.StatusOK, "No saved recipes."},
		{"missing", "user_missing", http.MethodGet, http.StatusNotFound, ""},
		{"legacy", "legacy-id", http.MethodGet, http.StatusNotFound, ""},
		{"invalid path", "user_../other", http.MethodGet, http.StatusNotFound, ""},
		{"method", "user_empty", http.MethodPost, http.StatusMethodNotAllowed, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/users/user_empty", nil)
			req.SetPathValue("id", tc.id)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}
			if tc.body != "" && !strings.Contains(rr.Body.String(), tc.body) {
				t.Fatalf("body missing %q: %s", tc.body, rr.Body.String())
			}
			if tc.name == "empty" && !strings.Contains(rr.Body.String(), "No cooking preferences saved.") {
				t.Fatalf("missing directive empty state: %s", rr.Body.String())
			}
			if tc.name == "empty" && !strings.Contains(rr.Body.String(), "No favorite store saved.") {
				t.Fatalf("missing favorite store empty state: %s", rr.Body.String())
			}
		})
	}
}

type failingAdminUserCache struct{ cache.ListCache }

func (f failingAdminUserCache) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("storage unavailable")
}

func TestAdminUserDetailPageStorageError(t *testing.T) {
	t.Parallel()
	storage := NewStorage(failingAdminUserCache{cache.NewFileCache(t.TempDir())})
	req := httptest.NewRequest(http.MethodGet, "/users/user_1", nil)
	req.SetPathValue("id", "user_1")
	rr := httptest.NewRecorder()
	AdminUserDetailPage(storage).ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
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
		ID:          "73f252fc-6116-4d48-9df1-b3cff4963f38",
		Email:       []string{"legacy@example.com"},
		ShoppingDay: time.Wednesday.String(),
	}); err != nil {
		t.Fatalf("update legacy user: %v", err)
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
