package main

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestMigrateShoppingListsApply(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())
	canonicalShopping := "5paGKJp_BFc"
	canonicalIngredients := "4MVQVRdNr8M"
	legacyShopping := toLegacyHash(t, canonicalShopping, legacyRecipeHashSeed)
	legacyIngredients := toLegacyHash(t, canonicalIngredients, legacyIngredientsHashSeed)

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
	if err := fc.Put(ctx, legacyShopping, string(shoppingJSON), cache.Unconditional()); err != nil {
		t.Fatalf("seed shopping list: %v", err)
	}
	if err := fc.Put(ctx, legacyShopping+".params", `{"location":{"id":"1"}}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed params: %v", err)
	}
	if err := fc.Put(ctx, legacyIngredients, `[{"description":"kale"}]`, cache.Unconditional()); err != nil {
		t.Fatalf("seed ingredients: %v", err)
	}
	if err := fc.Put(ctx, "recipe/abc", `{"title":"abc"}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed recipe key: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Copied != 3 {
		t.Fatalf("expected 3 copied keys, got %d", stats.Copied)
	}
	if stats.ShoppingListKeys != 1 || stats.ParamsKeys != 1 || stats.IngredientsKeys != 1 {
		t.Fatalf("expected shopping=1 params=1 ingredients=1, got shopping=%d params=%d ingredients=%d", stats.ShoppingListKeys, stats.ParamsKeys, stats.IngredientsKeys)
	}

	if _, err := fc.Get(ctx, legacyShopping); err != nil {
		t.Fatalf("expected old shopping list key to remain after copy: %v", err)
	}

	newBlob, err := fc.Get(ctx, "shoppinglist/"+canonicalShopping)
	if err != nil {
		t.Fatalf("expected moved shopping list: %v", err)
	}
	_ = newBlob.Close()

	if _, err := fc.Get(ctx, "params/"+canonicalShopping); err != nil {
		t.Fatalf("expected params copied to prefixed key: %v", err)
	}
	if _, err := fc.Get(ctx, "ingredients/"+canonicalIngredients); err != nil {
		t.Fatalf("expected ingredients copied to prefixed key: %v", err)
	}
}

func TestMigrateShoppingListsDryRun(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())
	canonicalShopping := "5paGKJp_BFc"
	legacyShopping := toLegacyHash(t, canonicalShopping, legacyRecipeHashSeed)

	if err := fc.Put(ctx, legacyShopping, `{"recipes":[{"title":"Soup"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed shopping list: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Copied != 1 {
		t.Fatalf("expected 1 planned copy, got %d", stats.Copied)
	}

	if _, err := fc.Get(ctx, legacyShopping); err != nil {
		t.Fatalf("expected original key to remain in dry-run: %v", err)
	}
	if _, err := fc.Get(ctx, "shoppinglist/"+canonicalShopping); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected destination key not created in dry-run, got err=%v", err)
	}
}

func TestMigrateShoppingListsApply_TransformsLegacySeededHashes(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())

	canonicalShopping := "5paGKJp_BFc"
	canonicalIngredients := "4MVQVRdNr8M"
	legacyShopping := toLegacyHash(t, canonicalShopping, legacyRecipeHashSeed)
	legacyIngredients := toLegacyHash(t, canonicalIngredients, legacyIngredientsHashSeed)

	if err := fc.Put(ctx, legacyShopping, `{"recipes":[{"title":"Soup"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed legacy shopping list: %v", err)
	}
	if err := fc.Put(ctx, legacyShopping+".params", `{"location":{"id":"1"}}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed legacy params: %v", err)
	}
	if err := fc.Put(ctx, legacyIngredients, `[{"description":"kale"}]`, cache.Unconditional()); err != nil {
		t.Fatalf("seed legacy ingredients: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Copied != 3 {
		t.Fatalf("expected 3 copied keys, got %d", stats.Copied)
	}

	if _, err := fc.Get(ctx, shoppingListPrefix+canonicalShopping); err != nil {
		t.Fatalf("expected canonical shopping list key: %v", err)
	}
	if _, err := fc.Get(ctx, paramsPrefix+canonicalShopping); err != nil {
		t.Fatalf("expected canonical params key: %v", err)
	}
	if _, err := fc.Get(ctx, ingredientsPrefix+canonicalIngredients); err != nil {
		t.Fatalf("expected canonical ingredients key: %v", err)
	}
}

func toLegacyHash(t *testing.T, canonicalHash string, seed string) string {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(canonicalHash)
	if err != nil {
		t.Fatalf("decode canonical hash %q: %v", canonicalHash, err)
	}
	legacyBytes := append([]byte(seed), decoded...)
	return base64.URLEncoding.EncodeToString(legacyBytes)
}
