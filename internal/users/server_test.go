package users

import (
	"careme/internal/cache"
	"careme/internal/locations"
	utypes "careme/internal/users/types"
	"context"
	"errors"
	"fmt"
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

type failingLocationGetter struct{}

func (f failingLocationGetter) GetLocationByID(_ context.Context, _ string) (*locations.Location, error) {
	return nil, errors.New("lookup failed")
}

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

func TestHandleUser_BlanksFavoriteStoreInHTMLWhenLocationLookupFails(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:   storage,
		userTmpl:  template.Must(template.New("user").Parse("favorite={{.User.FavoriteStore}} name={{.FavoriteStoreName}}")),
		locGetter: failingLocationGetter{},
		clerk:     testAuthClient{},
	}

	existing := &utypes.User{
		ID:            "user-1",
		Email:         []string{"user@example.com"},
		CreatedAt:     time.Now(),
		ShoppingDay:   "Saturday",
		FavoriteStore: "70500874",
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user", nil)
	rr := httptest.NewRecorder()
	s.handleUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "70500874") {
		t.Fatalf("expected favorite store to be blanked in template output, got %q", body)
	}
	if !strings.Contains(body, "favorite= name=") {
		t.Fatalf("expected favorite and favorite name to be blank in output, got %q", body)
	}

	user, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("expected user to remain stored, got error %v", err)
	}
	if user.FavoriteStore != "70500874" {
		t.Fatalf("expected persisted favorite store to stay unchanged, got %q", user.FavoriteStore)
	}
}

func TestFilterPastRecipes_AppliesQueryAndCap(t *testing.T) {
	t.Parallel()

	recipes := make([]utypes.Recipe, 0, 80)
	now := time.Now()
	for i := 0; i < 60; i++ {
		recipes = append(recipes, utypes.Recipe{
			Title:     fmt.Sprintf("Chicken Bowl %d", i),
			CreatedAt: now.Add(time.Duration(-i) * time.Minute),
		})
	}
	for i := 0; i < 20; i++ {
		recipes = append(recipes, utypes.Recipe{
			Title:     fmt.Sprintf("Soup %d", i),
			CreatedAt: now.Add(time.Duration(-60-i) * time.Minute),
		})
	}

	filtered := filterPastRecipes(recipes, "CHICKEN", maxDisplayedPastRecipes)
	if got, want := len(filtered), maxDisplayedPastRecipes; got != want {
		t.Fatalf("expected %d filtered recipes, got %d", want, got)
	}
	for i := range filtered {
		if !strings.Contains(strings.ToLower(filtered[i].Title), "chicken") {
			t.Fatalf("expected only chicken recipes, got %q", filtered[i].Title)
		}
	}

	unfiltered := filterPastRecipes(recipes, "", maxDisplayedPastRecipes)
	if got, want := len(unfiltered), maxDisplayedPastRecipes; got != want {
		t.Fatalf("expected %d unfiltered recipes, got %d", want, got)
	}
}

func TestHandleUser_PastTabIncludesSearchQueryAndCapsResults(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: template.Must(template.New("user").Parse("q={{.RecipeSearch}} count={{len .User.LastRecipes}}")),
		clerk:    testAuthClient{},
	}

	now := time.Now()
	recipes := make([]utypes.Recipe, 0, 80)
	for i := 0; i < 60; i++ {
		recipes = append(recipes, utypes.Recipe{
			Title:     fmt.Sprintf("Chicken Bowl %d", i),
			CreatedAt: now.Add(time.Duration(-i) * time.Minute),
		})
	}
	for i := 0; i < 20; i++ {
		recipes = append(recipes, utypes.Recipe{
			Title:     fmt.Sprintf("Soup %d", i),
			CreatedAt: now.Add(time.Duration(-60-i) * time.Minute),
		})
	}
	if err := storage.Update(&utypes.User{
		ID:          "user-1",
		Email:       []string{"user@example.com"},
		CreatedAt:   now,
		ShoppingDay: "Saturday",
		LastRecipes: recipes,
	}); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user?tab=past&q=chicken", nil)
	rr := httptest.NewRecorder()
	s.handleUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "q=chicken") {
		t.Fatalf("expected response to include search query, got %q", body)
	}
	if !strings.Contains(body, "count=50") {
		t.Fatalf("expected response to include capped count=50, got %q", body)
	}
}
