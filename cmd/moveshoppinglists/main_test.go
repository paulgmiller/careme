package main

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
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

func TestMigrateShoppingListsApply_DeletesSourceWhenDestinationExists(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())
	canonicalShopping := "5paGKJp_BFc"
	legacyShopping := toLegacyHash(t, canonicalShopping, legacyRecipeHashSeed)
	dstKey := shoppingListPrefix + canonicalShopping

	if err := fc.Put(ctx, legacyShopping, `{"recipes":[{"title":"Old"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed legacy shopping list: %v", err)
	}
	if err := fc.Put(ctx, dstKey, `{"recipes":[{"title":"Canonical"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed canonical shopping list: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Copied != 0 {
		t.Fatalf("expected 0 copied keys, got %d", stats.Copied)
	}
	if stats.SkippedExisting != 1 {
		t.Fatalf("expected 1 skipped existing key, got %d", stats.SkippedExisting)
	}

	if _, err := fc.Get(ctx, legacyShopping); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected legacy key deleted when destination exists, got err=%v", err)
	}

	r, err := fc.Get(ctx, dstKey)
	if err != nil {
		t.Fatalf("get canonical shopping list: %v", err)
	}
	body, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("read canonical shopping list: %v", err)
	}
	if string(body) != `{"recipes":[{"title":"Canonical"}]}` {
		t.Fatalf("expected destination payload unchanged, got %q", body)
	}
}

func TestMigrateShoppingListsDryRun_DoesNotDeleteSourceWhenDestinationExists(t *testing.T) {
	ctx := context.Background()
	fc := cache.NewFileCache(t.TempDir())
	canonicalShopping := "5paGKJp_BFc"
	legacyShopping := toLegacyHash(t, canonicalShopping, legacyRecipeHashSeed)
	dstKey := shoppingListPrefix + canonicalShopping

	if err := fc.Put(ctx, legacyShopping, `{"recipes":[{"title":"Old"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed legacy shopping list: %v", err)
	}
	if err := fc.Put(ctx, dstKey, `{"recipes":[{"title":"Canonical"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed canonical shopping list: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, fc, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if stats.Copied != 0 {
		t.Fatalf("expected 0 planned copies when destination exists, got %d", stats.Copied)
	}
	if stats.SkippedExisting != 1 {
		t.Fatalf("expected 1 skipped existing key, got %d", stats.SkippedExisting)
	}

	if _, err := fc.Get(ctx, legacyShopping); err != nil {
		t.Fatalf("expected legacy key to remain in dry-run, err=%v", err)
	}
}

func TestCopyKeyApply_DeletesSourceWhenPutReportsAlreadyExists(t *testing.T) {
	ctx := context.Background()
	srcKey := "legacy"
	dstKey := "shoppinglist/canonical"

	c := &raceAlreadyExistsCache{
		srcKey: srcKey,
		dstKey: dstKey,
		data: map[string]string{
			srcKey: `{"recipes":[{"title":"Old"}]}`,
			dstKey: `{"recipes":[{"title":"Canonical"}]}`,
		},
	}

	copied, skippedExisting, err := copyKey(ctx, c, srcKey, dstKey, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("copyKey: %v", err)
	}
	if copied != 0 {
		t.Fatalf("expected copied=0, got %d", copied)
	}
	if skippedExisting != 1 {
		t.Fatalf("expected skippedExisting=1, got %d", skippedExisting)
	}
	if _, ok := c.data[srcKey]; ok {
		t.Fatalf("expected source key deleted after concurrent destination existence")
	}
	if _, ok := c.data[dstKey]; !ok {
		t.Fatalf("expected destination key to remain")
	}
}

type raceAlreadyExistsCache struct {
	srcKey string
	dstKey string
	data   map[string]string
}

func (c *raceAlreadyExistsCache) Get(_ context.Context, key string) (io.ReadCloser, error) {
	v, ok := c.data[key]
	if !ok {
		return nil, cache.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(v)), nil
}

func (c *raceAlreadyExistsCache) Exists(_ context.Context, key string) (bool, error) {
	if key == c.dstKey {
		return false, nil
	}
	_, ok := c.data[key]
	return ok, nil
}

func (c *raceAlreadyExistsCache) Put(_ context.Context, key, value string, opts cache.PutOptions) error {
	if key == c.dstKey && opts.Condition == cache.PutIfNoneMatch {
		return cache.ErrAlreadyExists
	}
	c.data[key] = value
	return nil
}

func (c *raceAlreadyExistsCache) Delete(_ context.Context, key string) error {
	if _, ok := c.data[key]; !ok {
		return cache.ErrNotFound
	}
	delete(c.data, key)
	return nil
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
