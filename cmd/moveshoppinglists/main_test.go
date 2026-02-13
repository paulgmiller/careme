package main

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMigrateShoppingListsApply(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())

	shopping := ai.ShoppingList{
		ConversationID: "conv_123",
		Recipes: []ai.Recipe{
			{Title: "Tacos"},
		},
	}
	shoppingJSON, err := json.Marshal(shopping)
	if err != nil {
		t.Fatalf("marshal shopping list: %v", err)
	}
	if err := fc.Put(ctx, "hash123", string(shoppingJSON), cache.Unconditional()); err != nil {
		t.Fatalf("seed shopping list: %v", err)
	}
	if err := fc.Put(ctx, "hash123.params", `{"location":{"id":"1"}}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed params: %v", err)
	}
	if err := fc.Put(ctx, "ingredientshash", `[{"description":"kale"}]`, cache.Unconditional()); err != nil {
		t.Fatalf("seed ingredients: %v", err)
	}
	if err := fc.Put(ctx, "recipe/abc", `{"title":"abc"}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed recipe key: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Moved != 1 {
		t.Fatalf("expected 1 moved key, got %d", stats.Moved)
	}

	if _, err := fc.Get(ctx, "hash123"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected old shopping list key removed, got err=%v", err)
	}

	newBlob, err := fc.Get(ctx, "shoppinglist/hash123")
	if err != nil {
		t.Fatalf("expected moved shopping list: %v", err)
	}
	_ = newBlob.Close()

	if _, err := fc.Get(ctx, "hash123.params"); err != nil {
		t.Fatalf("expected params key unchanged: %v", err)
	}
	if _, err := fc.Get(ctx, "ingredientshash"); err != nil {
		t.Fatalf("expected ingredient key unchanged: %v", err)
	}
}

func TestMigrateShoppingListsDryRun(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())

	if err := fc.Put(ctx, "hash123", `{"recipes":[{"title":"Soup"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed shopping list: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Moved != 1 {
		t.Fatalf("expected 1 planned move, got %d", stats.Moved)
	}

	if _, err := fc.Get(ctx, "hash123"); err != nil {
		t.Fatalf("expected original key to remain in dry-run: %v", err)
	}
	if _, err := fc.Get(ctx, "shoppinglist/hash123"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected destination key not created in dry-run, got err=%v", err)
	}
}
