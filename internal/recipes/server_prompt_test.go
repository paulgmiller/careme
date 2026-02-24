package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/users"
	utypes "careme/internal/users/types"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

type instantGenerator struct{}

func (i instantGenerator) GenerateRecipes(_ context.Context, _ *generatorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{
		ConversationID: "conv-1",
		Recipes: []ai.Recipe{
			{Title: "Test Recipe", Description: "Test"},
		},
	}, nil
}

func (i instantGenerator) AskQuestion(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}

func (i instantGenerator) Ready(_ context.Context) error {
	return nil
}

func TestMergeInstructions(t *testing.T) {
	t.Run("profile only", func(t *testing.T) {
		got := mergeInstructions("Always include one vegetarian meal.", "")
		want := "Always include one vegetarian meal."
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("request only", func(t *testing.T) {
		got := mergeInstructions("", "No shellfish")
		want := "No shellfish"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("profile and request", func(t *testing.T) {
		got := mergeInstructions("Always include one vegetarian meal.", "No shellfish")
		want := "Always include one vegetarian meal.\n\nNo shellfish"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}

func TestHandleRecipes_MergesProfilePromptIntoSavedParams(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)

	user := &utypes.User{
		ID:               "mock-clerk-user-id",
		Email:            []string{"you@careme.cooking"},
		CreatedAt:        time.Now(),
		ShoppingDay:      "Saturday",
		GenerationPrompt: "Always include one vegetarian meal.",
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		storage:   storage,
		cache:     cacheStore,
		generator: instantGenerator{},
		locServer: staticLocationLookup{
			location: &locations.Location{
				ID:      "70500874",
				Name:    "Test Store",
				ZipCode: "10001",
				State:   "NY",
			},
		},
		clerk: auth.DefaultMock(),
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70500874&instructions=No+shellfish", nil)
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)
	s.Wait()

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}

	locationHeader := rr.Header().Get("Location")
	if locationHeader == "" {
		t.Fatal("expected redirect location header")
	}
	redirectURL, err := url.Parse(locationHeader)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", locationHeader, err)
	}
	hash := redirectURL.Query().Get("h")
	if hash == "" {
		t.Fatalf("expected hash in redirect location, got %q", locationHeader)
	}

	params, err := s.ParamsFromCache(context.Background(), hash)
	if err != nil {
		t.Fatalf("failed to load params by hash: %v", err)
	}

	want := "Always include one vegetarian meal.\n\nNo shellfish"
	if got := params.Instructions; got != want {
		t.Fatalf("expected instructions %q, got %q", want, got)
	}
}
