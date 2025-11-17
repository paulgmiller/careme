package users

import (
	"careme/internal/cache"
	"testing"
)

func TestUserStoreAdapter(t *testing.T) {
	// Create in-memory cache for testing
	c := cache.NewFileCache("/tmp/test-cache-adapter")
	storage := NewStorage(c)

	// Create a test user
	user := &User{
		ID:            "test-user-123",
		Email:         []string{"test@example.com"},
		FavoriteStore: "",
		ShoppingDay:   "Saturday",
	}

	if err := storage.Update(user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create adapter
	adapter := NewUserStoreAdapter(storage)

	// Test GetUserByID
	userID, favoriteStore, err := adapter.GetUserByID("test-user-123")
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if userID != "test-user-123" {
		t.Errorf("Expected userID to be 'test-user-123', got '%s'", userID)
	}
	if favoriteStore != "" {
		t.Errorf("Expected empty favoriteStore, got '%s'", favoriteStore)
	}

	// Test UpdateUserFavoriteStore
	err = adapter.UpdateUserFavoriteStore("test-user-123", "70100023")
	if err != nil {
		t.Fatalf("UpdateUserFavoriteStore failed: %v", err)
	}

	// Verify the update
	userID, favoriteStore, err = adapter.GetUserByID("test-user-123")
	if err != nil {
		t.Fatalf("GetUserByID after update failed: %v", err)
	}
	if favoriteStore != "70100023" {
		t.Errorf("Expected favoriteStore to be '70100023', got '%s'", favoriteStore)
	}

	// Verify using direct storage access
	updatedUser, err := storage.GetByID("test-user-123")
	if err != nil {
		t.Fatalf("Failed to get user from storage: %v", err)
	}
	if updatedUser.FavoriteStore != "70100023" {
		t.Errorf("Expected user.FavoriteStore to be '70100023', got '%s'", updatedUser.FavoriteStore)
	}
}
