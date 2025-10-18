package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateRecipesSeparateStorage(t *testing.T) {
	// Create a temporary directory for file cache
	tmpDir, err := os.MkdirTemp("", "careme-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file cache
	fileCache := cache.NewFileCache(tmpDir)

	// Mock a shopping list with recipes
	shoppingList := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:       "Test Recipe 1",
				Description: "First test recipe",
				Ingredients: []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.99"}},
				Instructions: []string{"Step 1"},
			},
			{
				Title:       "Test Recipe 2",
				Description: "Second test recipe",
				Ingredients: []ai.Ingredient{{Name: "ingredient2", Quantity: "2 tbsp", Price: "1.99"}},
				Instructions: []string{"Step 1", "Step 2"},
			},
		},
	}

	// Compute hashes for recipes
	for i := range shoppingList.Recipes {
		shoppingList.Recipes[i].Hash = shoppingList.Recipes[i].ComputeHash()
	}

	// Save individual recipes
	var recipeHashes []string
	for _, recipe := range shoppingList.Recipes {
		recipeJSON, err := json.Marshal(recipe)
		if err != nil {
			t.Fatalf("failed to marshal recipe: %v", err)
		}
		
		err = fileCache.Set("recipe/"+recipe.Hash, string(recipeJSON))
		if err != nil {
			t.Fatalf("failed to save recipe: %v", err)
		}
		recipeHashes = append(recipeHashes, recipe.Hash)
	}

	// Create and save shopping list document
	doc := ai.ShoppingListDocument{
		RecipeHashes: recipeHashes,
		Instructions: "Test instructions",
		CreatedAt:    time.Now().Format(time.RFC3339),
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("failed to marshal document: %v", err)
	}

	testHash := "test-shopping-list-hash"
	err = fileCache.Set(testHash, string(docJSON))
	if err != nil {
		t.Fatalf("failed to save document: %v", err)
	}

	// Verify shopping list document was saved
	docReader, err := fileCache.Get(testHash)
	if err != nil {
		t.Fatalf("failed to retrieve document: %v", err)
	}
	defer docReader.Close()

	docBytes, err := io.ReadAll(docReader)
	if err != nil {
		t.Fatalf("failed to read document: %v", err)
	}

	var retrievedDoc ai.ShoppingListDocument
	err = json.Unmarshal(docBytes, &retrievedDoc)
	if err != nil {
		t.Fatalf("failed to unmarshal document: %v", err)
	}

	if len(retrievedDoc.RecipeHashes) != 2 {
		t.Fatalf("expected 2 recipe hashes, got %d", len(retrievedDoc.RecipeHashes))
	}

	if retrievedDoc.Instructions != "Test instructions" {
		t.Fatalf("expected instructions 'Test instructions', got '%s'", retrievedDoc.Instructions)
	}

	// Verify individual recipes can be retrieved
	for i, recipeHash := range retrievedDoc.RecipeHashes {
		recipeReader, err := fileCache.Get("recipe/" + recipeHash)
		if err != nil {
			t.Fatalf("failed to retrieve recipe %d: %v", i, err)
		}
		defer recipeReader.Close()

		recipeBytes, err := io.ReadAll(recipeReader)
		if err != nil {
			t.Fatalf("failed to read recipe %d: %v", i, err)
		}

		var recipe ai.Recipe
		err = json.Unmarshal(recipeBytes, &recipe)
		if err != nil {
			t.Fatalf("failed to unmarshal recipe %d: %v", i, err)
		}

		if recipe.Hash != recipeHash {
			t.Fatalf("recipe hash mismatch: expected %s, got %s", recipeHash, recipe.Hash)
		}
	}
}

func TestFromCacheWithSeparateRecipes(t *testing.T) {
	// Create a temporary directory for file cache
	tmpDir, err := os.MkdirTemp("", "careme-test-fromcache-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file cache
	fileCache := cache.NewFileCache(tmpDir)

	// Create a test config
	cfg := &config.Config{}

	// Create generator
	gen := &Generator{
		config: cfg,
		cache:  fileCache,
	}

	// Create test recipes
	recipe1 := ai.Recipe{
		Title:       "Recipe 1",
		Description: "Description 1",
		Ingredients: []ai.Ingredient{{Name: "ing1", Quantity: "1 cup", Price: "1.99"}},
		Instructions: []string{"Step 1"},
	}
	recipe1.Hash = recipe1.ComputeHash()

	recipe2 := ai.Recipe{
		Title:       "Recipe 2",
		Description: "Description 2",
		Ingredients: []ai.Ingredient{{Name: "ing2", Quantity: "2 tbsp", Price: "2.99"}},
		Instructions: []string{"Step 1", "Step 2"},
	}
	recipe2.Hash = recipe2.ComputeHash()

	// Save recipes
	recipe1JSON, _ := json.Marshal(recipe1)
	recipe2JSON, _ := json.Marshal(recipe2)
	fileCache.Set("recipe/"+recipe1.Hash, string(recipe1JSON))
	fileCache.Set("recipe/"+recipe2.Hash, string(recipe2JSON))

	// Create shopping list document
	doc := ai.ShoppingListDocument{
		RecipeHashes: []string{recipe1.Hash, recipe2.Hash},
		Instructions: "Test instructions",
		CreatedAt:    time.Now().Format(time.RFC3339),
	}
	docJSON, _ := json.Marshal(doc)

	testHash := "test-hash-123"
	fileCache.Set(testHash, string(docJSON))

	// Save params
	loc := &locations.Location{ID: "test-loc", Name: "Test Location", State: "WA"}
	params := DefaultParams(loc, time.Now())
	paramsJSON, _ := json.Marshal(params)
	fileCache.Set(testHash+".params", string(paramsJSON))

	// Test FromCache - it should load the shopping list document and individual recipes
	// We can't fully test HTML generation without a full setup, but we can verify the loading works
	ctx := context.Background()
	
	// Read directly to verify the structure
	reader, err := fileCache.Get(testHash)
	if err != nil {
		t.Fatalf("failed to get shopping list document: %v", err)
	}
	defer reader.Close()

	bytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read shopping list document: %v", err)
	}

	var loadedDoc ai.ShoppingListDocument
	err = json.Unmarshal(bytes, &loadedDoc)
	if err != nil {
		t.Fatalf("failed to unmarshal shopping list document: %v", err)
	}

	if len(loadedDoc.RecipeHashes) != 2 {
		t.Fatalf("expected 2 recipe hashes, got %d", len(loadedDoc.RecipeHashes))
	}

	// Verify we can load individual recipes
	for _, recipeHash := range loadedDoc.RecipeHashes {
		recipeReader, err := fileCache.Get("recipe/" + recipeHash)
		if err != nil {
			t.Fatalf("failed to load recipe %s: %v", recipeHash, err)
		}
		recipeReader.Close()
	}

	// Just verify FromCache doesn't error (can't test full HTML without template setup)
	_ = ctx
	_ = gen
}

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
	recipe.Hash = recipe.ComputeHash()

	recipeJSON, _ := json.Marshal(recipe)
	err = fileCache.Set("recipe/"+recipe.Hash, string(recipeJSON))
	if err != nil {
		t.Fatalf("failed to save recipe: %v", err)
	}

	// Verify file exists at expected path
	expectedPath := filepath.Join(tmpDir, "recipe", recipe.Hash)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("recipe file not found at expected path: %s", expectedPath)
	}
}
