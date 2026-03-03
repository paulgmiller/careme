package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/users"
	utypes "careme/internal/users/types"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSaveRecipesToUserProfile(t *testing.T) {
	// Create temporary cache
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("failed to remove temp dir: %v", err)
		}
	})

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	// Create a test user
	testUser := &utypes.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Create server instance
	srv := &server{
		storage: storage,
	}

	// Create test recipes
	savedRecipe := ai.Recipe{
		Title:       "Test Recipe 1",
		Description: "A test recipe",
	}

	// Save recipes to user profile
	ctx := context.Background()
	if err := srv.saveRecipesToUserProfile(ctx, testUser, savedRecipe); err != nil {
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

	if updatedUser.LastRecipes[0].Title != savedRecipe.Title {
		t.Errorf("recipe title mismatch: expected %q, got %q", savedRecipe.Title, updatedUser.LastRecipes[0].Title)
	}
	if updatedUser.LastRecipes[0].Hash != savedRecipe.ComputeHash() {
		t.Errorf("recipe hash mismatch: expected %q, got %q", savedRecipe.ComputeHash(), updatedUser.LastRecipes[0].Hash)
	}

}

func TestSaveRecipesToUserProfile_NoDuplicates(t *testing.T) {
	// Create temporary cache
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("failed to remove temp dir: %v", err)
		}
	})

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	// Try to save the same recipe again (case-insensitive)
	savedRecipe := ai.Recipe{
		Title:       "test recipe 1", // lowercase version
		Description: "A test recipe",
	}
	// Create a test user with an existing recipe
	existingRecipe := utypes.Recipe{
		Title:     savedRecipe.Title,
		Hash:      savedRecipe.ComputeHash(),
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}
	testUser := &utypes.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{existingRecipe},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Create server instance
	srv := &server{
		storage: storage,
	}

	// Save recipes to user profile
	ctx := context.Background()
	if err := srv.saveRecipesToUserProfile(ctx, testUser, savedRecipe); err != nil {
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

func TestRemoveRecipeFromUserProfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "careme-test-user-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("failed to remove temp dir: %v", err)
		}
	})

	tmpCache := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(tmpCache)

	keep := utypes.Recipe{
		Title:     "Keep Recipe",
		Hash:      "keep-hash",
		CreatedAt: time.Now().Add(-24 * time.Hour),
	}
	remove := utypes.Recipe{
		Title:     "Remove Recipe",
		Hash:      "remove-hash",
		CreatedAt: time.Now(),
	}
	testUser := &utypes.User{
		ID:          "test-user-id",
		Email:       []string{"test@example.com"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{keep, remove},
	}
	if err := storage.Update(testUser); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	srv := &server{
		storage: storage,
	}

	if err := srv.removeRecipeFromUserProfile(context.Background(), *testUser, remove.Hash); err != nil {
		t.Fatalf("failed to remove recipe from user profile: %v", err)
	}

	updatedUser, err := storage.GetByID(testUser.ID)
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}
	if len(updatedUser.LastRecipes) != 1 {
		t.Fatalf("expected 1 recipe after removal, got %d", len(updatedUser.LastRecipes))
	}
	if updatedUser.LastRecipes[0].Hash != keep.Hash {
		t.Fatalf("expected remaining recipe hash %q, got %q", keep.Hash, updatedUser.LastRecipes[0].Hash)
	}
}
