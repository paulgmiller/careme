package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/users"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSaveSavedRecipesToUserProfile(t *testing.T) {
	// Create temporary cache
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
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

	// Create server instance
	srv := &server{
		storage: storage,
	}

	// Create test recipes
	savedRecipes := []ai.Recipe{
		{
			Title:       "Test Recipe 1",
			Description: "A test recipe",
		},
		{
			Title:       "Test Recipe 2",
			Description: "Another test recipe",
		},
	}

	// Save recipes to user profile
	ctx := context.Background()
	if err := srv.saveSavedRecipesToUserProfile(ctx, testUser, savedRecipes); err != nil {
		t.Fatalf("failed to save recipes to user profile: %v", err)
	}

	// Verify recipes were added to user profile
	updatedUser, err := storage.GetByID(testUser.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 2 {
		t.Fatalf("expected 2 recipes in user profile, got %d", len(updatedUser.LastRecipes))
	}

	// Verify recipe titles match
	for i, recipe := range savedRecipes {
		if updatedUser.LastRecipes[i].Title != recipe.Title {
			t.Errorf("recipe %d title mismatch: expected %q, got %q", i, recipe.Title, updatedUser.LastRecipes[i].Title)
		}
		if updatedUser.LastRecipes[i].Hash != recipe.ComputeHash() {
			t.Errorf("recipe %d hash mismatch", i)
		}
	}
}

func TestSaveSavedRecipesToUserProfile_NoDuplicates(t *testing.T) {
	// Create temporary cache
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	// Create a test user with an existing recipe
	existingRecipe := users.Recipe{
		Title:     "Test Recipe 1",
		Hash:      "existing-hash",
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}
	testUser := &users.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []users.Recipe{existingRecipe},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Create server instance
	srv := &server{
		storage: storage,
	}

	// Try to save the same recipe again (case-insensitive)
	savedRecipes := []ai.Recipe{
		{
			Title:       "test recipe 1", // lowercase version
			Description: "A test recipe",
		},
		{
			Title:       "Test Recipe 2",
			Description: "Another test recipe",
		},
	}

	// Save recipes to user profile
	ctx := context.Background()
	if err := srv.saveSavedRecipesToUserProfile(ctx, testUser, savedRecipes); err != nil {
		t.Fatalf("failed to save recipes to user profile: %v", err)
	}

	// Verify only new recipe was added
	updatedUser, err := storage.GetByID(testUser.ID)
	if err != nil {
		t.Fatalf("failed to retrieve updated user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 2 {
		t.Fatalf("expected 2 recipes in user profile (1 existing + 1 new), got %d", len(updatedUser.LastRecipes))
	}

	// Verify the existing recipe wasn't duplicated
	found := false
	for _, recipe := range updatedUser.LastRecipes {
		if strings.EqualFold(recipe.Title, "Test Recipe 1") {
			if found {
				t.Error("duplicate recipe found in user profile")
			}
			found = true
		}
	}
}

func TestSaveSavedRecipesToUserProfile_InvalidUser(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	srv := &server{
		storage: storage,
	}

	savedRecipes := []ai.Recipe{
		{
			Title:       "Test Recipe",
			Description: "A test recipe",
		},
	}

	ctx := context.Background()

	// Test with nil user
	if err := srv.saveSavedRecipesToUserProfile(ctx, nil, savedRecipes); err == nil {
		t.Error("expected error with nil user, got nil")
	}

	// Test with user with empty ID
	emptyUser := &users.User{ID: ""}
	if err := srv.saveSavedRecipesToUserProfile(ctx, emptyUser, savedRecipes); err == nil {
		t.Error("expected error with empty user ID, got nil")
	}
}
