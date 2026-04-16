package users

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/routing"
	"careme/internal/templates"

	utypes "careme/internal/users/types"
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

func (t testAuthClient) Register(_ routing.Registrar) {}

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
	if got := rr.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected no-store cache header, got %q", got)
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

func TestHandleUser_PastRecipesShowCookedIndicator(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: templates.User,
		clerk:    testAuthClient{},
	}
	now := time.Now()

	existing := &utypes.User{
		ID:          "user-1",
		Email:       []string{"user@example.com"},
		CreatedAt:   now,
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{
			{Title: "Cooked Pasta", Hash: "hash-cooked", CreatedAt: now.Add(-2 * time.Hour)},
			{Title: "Cooked No Rating", Hash: "hash-cooked-unrated", CreatedAt: now.Add(-90 * time.Minute)},
			{Title: "Saved Soup", Hash: "hash-saved", CreatedAt: now.Add(-1 * time.Hour)},
			{Title: "Cooked Three Weeks", Hash: "hash-cooked-three-weeks", CreatedAt: now.Add(-21 * 24 * time.Hour)},
			{Title: "Saved Three Weeks", Hash: "hash-saved-three-weeks", CreatedAt: now.Add(-21 * 24 * time.Hour)},
			{Title: "Cooked Five Weeks", Hash: "hash-cooked-five-weeks", CreatedAt: now.Add(-35 * 24 * time.Hour)},
		},
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	feedbackIO := feedback.NewIO(cacheStore)
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-cooked", feedback.Feedback{Cooked: true, Stars: 4, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed cooked feedback: %v", err)
	}
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-cooked-unrated", feedback.Feedback{Cooked: true, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed unrated cooked feedback: %v", err)
	}
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-saved", feedback.Feedback{Cooked: false, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed uncooked feedback: %v", err)
	}
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-cooked-three-weeks", feedback.Feedback{Cooked: true, Stars: 2, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed three-week cooked feedback: %v", err)
	}
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-saved-three-weeks", feedback.Feedback{Cooked: false, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed three-week saved feedback: %v", err)
	}
	if err := feedbackIO.SaveFeedback(t.Context(), "hash-cooked-five-weeks", feedback.Feedback{Cooked: true, Stars: 5, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to seed five-week cooked feedback: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/user?tab=past", nil)
	rr := httptest.NewRecorder()

	s.handleUser(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `/static/htmx@2.0.8.js`) {
		t.Fatalf("expected user page to include htmx script, got body: %s", body)
	}
	if !strings.Contains(body, `Cooked Pasta</a> <span aria-label="Rated 4 stars" title="Rated 4 stars">⭐⭐⭐⭐</span>`) {
		t.Fatalf("expected cooked recipe to render 4 stars, got body: %s", body)
	}
	if !strings.Contains(body, `Cooked No Rating</a> <span aria-label="Cooked" title="Cooked">🔪</span>`) {
		t.Fatalf("expected unrated cooked recipe to render 1 star, got body: %s", body)
	}
	if strings.Contains(body, `Saved Soup</a> <span aria-label="Rated`) {
		t.Fatalf("expected uncooked saved recipe not to render stars, got body: %s", body)
	}
	if !strings.Contains(body, `Cooked Three Weeks</a> <span aria-label="Rated 2 stars" title="Rated 2 stars">⭐⭐</span>`) {
		t.Fatalf("expected cooked recipe from three weeks ago to remain visible, got body: %s", body)
	}
	if strings.Contains(body, `Saved Three Weeks`) {
		t.Fatalf("expected uncooked saved recipe older than two weeks to be hidden, got body: %s", body)
	}
	if strings.Contains(body, `Cooked Five Weeks`) {
		t.Fatalf("expected cooked recipe older than four weeks to be hidden, got body: %s", body)
	}
	if !strings.Contains(body, `hx-post="/user/recipes/remove"`) {
		t.Fatalf("expected remove recipe form to post via htmx, got body: %s", body)
	}
	if !strings.Contains(body, `hx-target="closest li"`) || !strings.Contains(body, `hx-swap="delete"`) {
		t.Fatalf("expected remove recipe form to delete only the matching row, got body: %s", body)
	}
}

func TestHandleRemoveUserRecipe_RejectsNonHTMXRequests(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: templates.User,
		clerk:    testAuthClient{},
	}

	keep := utypes.Recipe{Title: "Keep Me", Hash: "hash-keep", CreatedAt: time.Now().Add(-2 * time.Hour).Round(0)}
	remove := utypes.Recipe{Title: "Remove Me", Hash: "hash-remove", CreatedAt: time.Now().Add(-1 * time.Hour).Round(0)}
	existing := &utypes.User{
		ID:          "user-1",
		Email:       []string{"user@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{keep, remove},
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	form := url.Values{
		"hash": {remove.Hash},
	}
	req := httptest.NewRequest(http.MethodPost, "/user/recipes/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handleRemoveUserRecipe(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	updated, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("failed to fetch updated user: %v", err)
	}
	if len(updated.LastRecipes) != 2 {
		t.Fatalf("expected recipes to remain unchanged, got %d", len(updated.LastRecipes))
	}
	gotTitles := []string{updated.LastRecipes[0].Title, updated.LastRecipes[1].Title}
	if !strings.Contains(strings.Join(gotTitles, ","), keep.Title) || !strings.Contains(strings.Join(gotTitles, ","), remove.Title) {
		t.Fatalf("expected recipes to remain unchanged, got %#v", updated.LastRecipes)
	}
}

func TestHandleRemoveUserRecipe_HTMXRemovesMatchingRecipeWithoutRedirect(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	s := &server{
		storage:  storage,
		userTmpl: templates.User,
		clerk:    testAuthClient{},
	}

	keep := utypes.Recipe{Title: "Keep Me", Hash: "hash-keep", CreatedAt: time.Now().Add(-2 * time.Hour).Round(0)}
	remove := utypes.Recipe{Title: "Remove Me", Hash: "hash-remove", CreatedAt: time.Now().Add(-1 * time.Hour).Round(0)}
	existing := &utypes.User{
		ID:          "user-1",
		Email:       []string{"user@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{keep, remove},
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	form := url.Values{
		"hash": {remove.Hash},
	}
	req := httptest.NewRequest(http.MethodPost, "/user/recipes/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	s.handleRemoveUserRecipe(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "" {
		t.Fatalf("expected no redirect location for htmx request, got %q", got)
	}

	updated, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("failed to fetch updated user: %v", err)
	}
	if len(updated.LastRecipes) != 1 {
		t.Fatalf("expected one recipe after removal, got %d", len(updated.LastRecipes))
	}
	if updated.LastRecipes[0].Title != keep.Title {
		t.Fatalf("expected kept recipe %q, got %q", keep.Title, updated.LastRecipes[0].Title)
	}
}
