package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/users"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHandleFinalize_Success(t *testing.T) {
	// Create temporary cache
	tmpDir, err := os.MkdirTemp("", "careme-test-finalize-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	// Create a test user
	testUser := &users.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []users.Recipe{},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Create test recipes and store them in cache
	recipe1 := ai.Recipe{
		Title:       "Test Recipe 1",
		Description: "A test recipe",
		Ingredients: []ai.Ingredient{
			{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"},
		},
		Instructions: []string{"Step 1"},
	}
	recipe2 := ai.Recipe{
		Title:       "Test Recipe 2",
		Description: "Another test recipe",
		Ingredients: []ai.Ingredient{
			{Name: "ingredient2", Quantity: "2 cups", Price: "3.00"},
		},
		Instructions: []string{"Step 1"},
	}

	// Store recipes in cache
	hash1 := recipe1.ComputeHash()
	hash2 := recipe2.ComputeHash()

	recipe1JSON, _ := json.Marshal(recipe1)
	recipe2JSON, _ := json.Marshal(recipe2)

	if err := tmpCache.Set(context.Background(), recipeCachePrefix+hash1, string(recipe1JSON)); err != nil {
		t.Fatalf("failed to cache recipe1: %v", err)
	}
	if err := tmpCache.Set(context.Background(), recipeCachePrefix+hash2, string(recipe2JSON)); err != nil {
		t.Fatalf("failed to cache recipe2: %v", err)
	}

	// Create server instance
	srv := &server{
		recipeio: recipeio{Cache: tmpCache},
		storage:  storage,
	}

	// Create request with saved recipe hashes
	form := url.Values{}
	form.Add("saved", hash1)
	form.Add("saved", hash2)

	req := httptest.NewRequest(http.MethodPost, "/recipes/finalize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Set user cookie
	cookie := &http.Cookie{
		Name:  users.CookieName,
		Value: testUser.ID,
	}
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.handleFinalize(w, req)

	// Check redirect
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected status %d, got %d", http.StatusSeeOther, w.Code)
	}

	// Verify recipes were added to user profile
	updatedUser, err := storage.GetByID(testUser.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 2 {
		t.Errorf("expected 2 recipes in user profile, got %d", len(updatedUser.LastRecipes))
	}

	// Verify recipe titles
	titles := make(map[string]bool)
	for _, recipe := range updatedUser.LastRecipes {
		titles[recipe.Title] = true
	}
	if !titles["Test Recipe 1"] || !titles["Test Recipe 2"] {
		t.Error("recipes not correctly saved to user profile")
	}
}

func TestHandleFinalize_NoRecipes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "careme-test-finalize-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	testUser := &users.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []users.Recipe{},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	srv := &server{
		recipeio: recipeio{Cache: tmpCache},
		storage:  storage,
	}

	// Create request with no saved recipes
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/recipes/finalize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	cookie := &http.Cookie{
		Name:  users.CookieName,
		Value: testUser.ID,
	}
	req.AddCookie(cookie)

	w := httptest.NewRecorder()
	srv.handleFinalize(w, req)

	// Should redirect even with no recipes
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected status %d, got %d", http.StatusSeeOther, w.Code)
	}

	// Verify no recipes were added
	updatedUser, err := storage.GetByID(testUser.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 0 {
		t.Errorf("expected 0 recipes in user profile, got %d", len(updatedUser.LastRecipes))
	}
}

func TestHandleFinalize_NoUser(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "careme-test-finalize-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	srv := &server{
		recipeio: recipeio{Cache: tmpCache},
		storage:  storage,
	}

	// Create request without user cookie
	form := url.Values{}
	form.Add("saved", "somehash")

	req := httptest.NewRequest(http.MethodPost, "/recipes/finalize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	srv.handleFinalize(w, req)

	// Should return 401 when no user
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}
