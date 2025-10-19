package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecipeFileNaming(t *testing.T) {
	// Verify that recipe files are stored with "recipe/" prefix
	tmpDir, err := os.MkdirTemp("", "careme-test-naming-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fileCache := cache.NewFileCache(tmpDir)

	recipe := ai.Recipe{
		Title: "Test Recipe",
	}

	hash := recipe.ComputeHash()
	recipeJSON, _ := json.Marshal(recipe)
	err = fileCache.Set("recipe/"+hash, string(recipeJSON))
	if err != nil {
		t.Fatalf("failed to save recipe: %v", err)
	}

	// Verify file exists at expected path
	expectedPath := filepath.Join(tmpDir, "recipe", hash)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("recipe file not found at expected path: %s", expectedPath)
	}
}
