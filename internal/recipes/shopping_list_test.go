package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/users"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestUpdateShoppingList(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	c := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(c)

	// Create a test user
	user, err := storage.FindOrCreateByEmail("test@example.com")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// Create test recipes
	recipe1 := ai.Recipe{
		Title:       "Test Recipe 1",
		Description: "First test recipe",
		Ingredients: []ai.Ingredient{
			{Name: "Ingredient A", Quantity: "1 cup", Price: "$2"},
			{Name: "Ingredient B", Quantity: "2 tbsp", Price: "$1"},
		},
		Instructions: []string{"Step 1", "Step 2"},
		Health:       "Healthy",
		DrinkPairing: "Water",
	}

	recipe2 := ai.Recipe{
		Title:       "Test Recipe 2",
		Description: "Second test recipe",
		Ingredients: []ai.Ingredient{
			{Name: "Ingredient C", Quantity: "1 lb", Price: "$5"},
			{Name: "Ingredient D", Quantity: "3 oz", Price: "$3"},
		},
		Instructions: []string{"Step A", "Step B"},
		Health:       "Very healthy",
		DrinkPairing: "Tea",
	}

	// Save recipes to cache
	recipeHash1 := recipe1.ComputeHash()
	recipeHash2 := recipe2.ComputeHash()

	recipe1JSON, _ := json.Marshal(recipe1)
	recipe2JSON, _ := json.Marshal(recipe2)

	if err := c.Set(ctx, "recipe/"+recipeHash1, string(recipe1JSON)); err != nil {
		t.Fatalf("failed to save recipe 1 to cache: %v", err)
	}
	if err := c.Set(ctx, "recipe/"+recipeHash2, string(recipe2JSON)); err != nil {
		t.Fatalf("failed to save recipe 2 to cache: %v", err)
	}

	// Add recipes to user's last recipes
	user.LastRecipes = []users.Recipe{
		{Title: recipe1.Title, Hash: recipeHash1, CreatedAt: time.Now()},
		{Title: recipe2.Title, Hash: recipeHash2, CreatedAt: time.Now()},
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	// Create server with mock location server
	locServer := &mockLocServer{}
	s := &server{
		recipeio:  recipeio{Cache: c},
		storage:   storage,
		locServer: locServer,
	}

	// Update shopping list
	if err := s.UpdateShoppingList(ctx, user.ID); err != nil {
		t.Fatalf("failed to update shopping list: %v", err)
	}

	// Reload user and verify shopping list
	user, err = storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}

	if len(user.ShoppingList) != 4 {
		t.Errorf("expected 4 items in shopping list, got %d", len(user.ShoppingList))
	}

	// Verify ingredients are in shopping list
	expectedItems := map[string]bool{
		"Ingredient A": false,
		"Ingredient B": false,
		"Ingredient C": false,
		"Ingredient D": false,
	}

	for _, item := range user.ShoppingList {
		if _, ok := expectedItems[item.Name]; ok {
			expectedItems[item.Name] = true
		}
	}

	for name, found := range expectedItems {
		if !found {
			t.Errorf("ingredient %s not found in shopping list", name)
		}
	}

	// Verify recipe titles are attached
	for _, item := range user.ShoppingList {
		if item.RecipeTitle == "" {
			t.Errorf("ingredient %s has no recipe title", item.Name)
		}
	}
}

func TestUpdateShoppingList_EmptyRecipes(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	c := cache.NewFileCache(tmpDir)
	storage := users.NewStorage(c)

	// Create a test user with no saved recipes
	user, err := storage.FindOrCreateByEmail("test2@example.com")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	locServer := &mockLocServer{}
	s := &server{
		recipeio:  recipeio{Cache: c},
		storage:   storage,
		locServer: locServer,
	}

	// Update shopping list
	if err := s.UpdateShoppingList(ctx, user.ID); err != nil {
		t.Fatalf("failed to update shopping list: %v", err)
	}

	// Reload user and verify shopping list is empty
	user, err = storage.GetByID(user.ID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}

	if len(user.ShoppingList) != 0 {
		t.Errorf("expected empty shopping list, got %d items", len(user.ShoppingList))
	}
}

type mockLocServer struct{}

func (m *mockLocServer) GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error) {
	return &locations.Location{
		ID:      locationID,
		Name:    "Test Store",
		Address: "123 Test St",
	}, nil
}
