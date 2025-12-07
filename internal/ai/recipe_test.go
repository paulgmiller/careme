package ai

import (
	"testing"
)

func TestRecipeComputeHash(t *testing.T) {
	recipe := Recipe{
		Title:       "Test Recipe",
		Description: "A delicious test recipe",
		Ingredients: []Ingredient{
			{Name: "Ingredient 1", Quantity: "1 cup", Price: "2.99"},
			{Name: "Ingredient 2", Quantity: "2 tbsp", Price: "0.99"},
		},
		Instructions: []string{"Step 1", "Step 2"},
		Health:       "Healthy",
		DrinkPairing: "Red Wine",
	}

	hash1 := recipe.ComputeHash()
	if hash1 == "" {
		t.Fatal("hash should not be empty")
	}

	// Hash should be consistent
	hash2 := recipe.ComputeHash()
	if hash1 != hash2 {
		t.Fatalf("hash should be consistent: %s != %s", hash1, hash2)
	}

	// Different recipe should have different hash
	recipe2 := recipe
	recipe2.Title = "Different Recipe"
	hash3 := recipe2.ComputeHash()
	if hash1 == hash3 {
		t.Fatalf("different recipes should have different hashes")
	}
}

func TestRecipeHashLength(t *testing.T) {
	recipe := Recipe{
		Title: "Simple Recipe",
	}

	hash := recipe.ComputeHash()
	// SHA256 produces 64 hex characters
	if len(hash) != 64 {
		t.Fatalf("expected hash length of 64, got %d", len(hash))
	}
}

func TestShoppingListConversationID(t *testing.T) {
	list := ShoppingList{
		Recipes: []Recipe{
			{Title: "Recipe 1"},
			{Title: "Recipe 2"},
		},
		ConversationID: "test-conversation-id-123",
	}

	if list.ConversationID != "test-conversation-id-123" {
		t.Fatalf("expected conversation ID to be 'test-conversation-id-123', got '%s'", list.ConversationID)
	}

	// Test that empty conversation ID is allowed
	list2 := ShoppingList{
		Recipes: []Recipe{{Title: "Recipe"}},
	}
	if list2.ConversationID != "" {
		t.Fatalf("expected empty conversation ID, got '%s'", list2.ConversationID)
	}
}
