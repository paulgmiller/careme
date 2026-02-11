package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/users"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectToHash(t *testing.T) {
	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()
	// Create a dummy request
	req := httptest.NewRequest("GET", "/dummy", nil)

	hash := "testhash"
	redirectToHash(rr, req, hash, true)

	// Check the status code
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusSeeOther)
	}

	// Check the Location header
	expectedLocation := fmt.Sprintf("/recipes?h=%s&start=", hash)
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, expectedLocation) {
		t.Errorf("handler returned wrong location: got %v want prefix %v", location, expectedLocation)
	}
}

type noSessionAuth struct{}

func (n noSessionAuth) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	return "", nil
}

func (n noSessionAuth) GetUserIDFromRequest(r *http.Request) (string, error) {
	return "", auth.ErrNoSession
}

func (n noSessionAuth) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (n noSessionAuth) Register(mux *http.ServeMux) {}

func TestHandleQuestion_RequiresSignedInUser(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

type signedInAuth struct{}

func (s signedInAuth) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	return "user@example.com", nil
}

func (s signedInAuth) GetUserIDFromRequest(r *http.Request) (string, error) {
	return "user-1", nil
}

func (s signedInAuth) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (s signedInAuth) Register(mux *http.ServeMux) {}

type questionGenerator struct{}

func (q questionGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{}, nil
}

func (q questionGenerator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	return "Try chicken thighs at the same cook time.", nil
}

func (q questionGenerator) Ready(ctx context.Context) error {
	return nil
}

func TestHandleQuestion_HTMXReturnsThreadFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		storage:   users.NewStorage(cacheStore),
		clerk:     signedInAuth{},
		generator: questionGenerator{},
	}

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "" {
		t.Fatalf("expected no redirect location for HTMX request, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="question-thread"`) {
		t.Fatalf("expected thread fragment in response, got body: %s", body)
	}
	if !strings.Contains(body, "Can I swap the protein?") {
		t.Fatalf("expected question in response, got body: %s", body)
	}
	if !strings.Contains(body, "Try chicken thighs at the same cook time.") {
		t.Fatalf("expected answer in response, got body: %s", body)
	}
}

func TestHandleQuestion_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/" {
		t.Fatalf("expected HX-Redirect to /, got %q", got)
	}
}
