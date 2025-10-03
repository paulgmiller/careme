package users

import (
	"fmt"
	"os"
	"testing"
	"time"

	"careme/internal/cache"
)

func TestSaveRecipe(t *testing.T) {
	// Create cache directory for tests
	if err := os.MkdirAll("../../cache", 0755); err != nil {
		t.Fatalf("failed to create cache directory: %v", err)
	}
	defer os.RemoveAll("../../cache")

	c, err := cache.MakeCache()
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	storage := NewStorage(c)

	// Create a test user
	user := &User{
		ID:            "test-user-123",
		Email:         []string{"test@example.com"},
		CreatedAt:     time.Now(),
		FavoriteStore: "12345",
		ShoppingDay:   "Monday",
	}

	// Save the user
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Save a recipe
	err = storage.SaveRecipe(user.ID, "Test Recipe", "hash123", "location456", "2025-01-15")
	if err != nil {
		t.Fatalf("failed to save recipe: %v", err)
	}

	// Retrieve the user and check recipes
	updatedUser, err := storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 1 {
		t.Fatalf("expected 1 recipe, got %d", len(updatedUser.LastRecipes))
	}

	recipe := updatedUser.LastRecipes[0]
	if recipe.Title != "Test Recipe" {
		t.Errorf("expected title 'Test Recipe', got '%s'", recipe.Title)
	}
	if recipe.Hash != "hash123" {
		t.Errorf("expected hash 'hash123', got '%s'", recipe.Hash)
	}
	if recipe.Location != "location456" {
		t.Errorf("expected location 'location456', got '%s'", recipe.Location)
	}
	if recipe.Date != "2025-01-15" {
		t.Errorf("expected date '2025-01-15', got '%s'", recipe.Date)
	}

	// Save the same recipe again (should be idempotent)
	err = storage.SaveRecipe(user.ID, "Test Recipe", "hash123", "location456", "2025-01-15")
	if err != nil {
		t.Fatalf("failed to save duplicate recipe: %v", err)
	}

	updatedUser, err = storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to get user after duplicate save: %v", err)
	}

	if len(updatedUser.LastRecipes) != 1 {
		t.Fatalf("expected 1 recipe after duplicate save, got %d", len(updatedUser.LastRecipes))
	}

	// Save a different recipe
	err = storage.SaveRecipe(user.ID, "Another Recipe", "hash456", "location789", "2025-01-16")
	if err != nil {
		t.Fatalf("failed to save second recipe: %v", err)
	}

	updatedUser, err = storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to get user after second save: %v", err)
	}

	if len(updatedUser.LastRecipes) != 2 {
		t.Fatalf("expected 2 recipes, got %d", len(updatedUser.LastRecipes))
	}

	// Verify the newest recipe is first
	if updatedUser.LastRecipes[0].Title != "Another Recipe" {
		t.Errorf("expected first recipe to be 'Another Recipe', got '%s'", updatedUser.LastRecipes[0].Title)
	}
}

func TestSaveRecipeLimit(t *testing.T) {
	// Create cache directory for tests
	if err := os.MkdirAll("../../cache", 0755); err != nil {
		t.Fatalf("failed to create cache directory: %v", err)
	}
	defer os.RemoveAll("../../cache")

	c, err := cache.MakeCache()
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	storage := NewStorage(c)

	// Create a test user
	user := &User{
		ID:            "test-user-456",
		Email:         []string{"test2@example.com"},
		CreatedAt:     time.Now(),
		FavoriteStore: "12345",
		ShoppingDay:   "Tuesday",
	}

	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Save 25 recipes (more than the limit of 20)
	for i := 0; i < 25; i++ {
		title := "Recipe " + string(rune('A'+i%26))
		hash := fmt.Sprintf("hash%d", i)
		err = storage.SaveRecipe(user.ID, title, hash, "loc", "2025-01-01")
		if err != nil {
			t.Fatalf("failed to save recipe %d: %v", i, err)
		}
	}

	// Retrieve the user and verify only 20 recipes are kept
	updatedUser, err := storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	if len(updatedUser.LastRecipes) != 20 {
		t.Fatalf("expected 20 recipes (limit), got %d", len(updatedUser.LastRecipes))
	}
}
