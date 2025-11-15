package ai

import (
	"strings"
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

func TestEncodeIngredientsToTOON(t *testing.T) {
	// Test that the function works with gotoon library
	ingredients := []map[string]interface{}{
		{
			"brand":        "Kroger",
			"description":  "Fresh Chicken Breast",
			"size":         "1 lb",
			"regularPrice": 8.99,
			"salePrice":    6.99,
		},
	}

	result := encodeIngredientsToTOON(ingredients)

	// Just verify it produces TOON format (has the ingredients header)
	if !strings.Contains(result, "ingredients[") {
		t.Errorf("Expected TOON format with 'ingredients[' header, got: %s", result)
	}
}

func TestEncodeIngredientsToTOON_Empty(t *testing.T) {
	result := encodeIngredientsToTOON([]interface{}{})

	if result != "ingredients[0]:" {
		t.Errorf("Expected 'ingredients[0]:', got: %s", result)
	}
}
