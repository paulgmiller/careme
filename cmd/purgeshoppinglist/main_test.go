package main

import (
	"bytes"
	"careme/internal/cache"
	"context"
	"errors"
	"testing"
)

func TestPurgeInvalidShoppingListsApply(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fc := cache.NewFileCache(".")

	if err := fc.Put(ctx, "shoppinglist/valid", `{"recipes":[{"title":"Soup"}]}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed valid shopping list: %v", err)
	}
	if err := fc.Put(ctx, "shoppinglist/invalid", `{"recipes":`, cache.Unconditional()); err != nil {
		t.Fatalf("seed invalid shopping list: %v", err)
	}

	stats, err := purgeInvalidShoppingLists(ctx, fc, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("purge invalid shopping lists: %v", err)
	}
	if stats.Found != 2 || stats.Valid != 1 || stats.Invalid != 1 || stats.Deleted != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.WouldDelete != 0 || stats.DeleteErrors != 0 {
		t.Fatalf("unexpected dry-run/delete errors stats: %+v", stats)
	}

	if _, err := fc.Get(ctx, "shoppinglist/valid"); err != nil {
		t.Fatalf("expected valid key to remain: %v", err)
	}
	if _, err := fc.Get(ctx, "shoppinglist/invalid"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected invalid key removed, got err=%v", err)
	}
}

func TestPurgeInvalidShoppingListsDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fc := cache.NewFileCache(".")

	if err := fc.Put(ctx, "shoppinglist/invalid", `{"recipes":`, cache.Unconditional()); err != nil {
		t.Fatalf("seed invalid shopping list: %v", err)
	}

	stats, err := purgeInvalidShoppingLists(ctx, fc, false, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("purge invalid shopping lists: %v", err)
	}
	if stats.Found != 1 || stats.Valid != 0 || stats.Invalid != 1 || stats.WouldDelete != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.Deleted != 0 || stats.DeleteErrors != 0 {
		t.Fatalf("unexpected apply/delete errors stats: %+v", stats)
	}

	if _, err := fc.Get(ctx, "shoppinglist/invalid"); err != nil {
		t.Fatalf("expected invalid key to remain in dry-run: %v", err)
	}
}

func TestPurgeInvalidShoppingListsHandlesPrefixedListResults(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	fc := cache.NewFileCache(".")
	prefixed := prefixedListCache{FileCache: fc}

	if err := fc.Put(ctx, "shoppinglist/invalid", `{"recipes":`, cache.Unconditional()); err != nil {
		t.Fatalf("seed invalid shopping list: %v", err)
	}

	stats, err := purgeInvalidShoppingLists(ctx, prefixed, true, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("purge invalid shopping lists: %v", err)
	}
	if stats.Found != 1 || stats.Invalid != 1 || stats.Deleted != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if _, err := fc.Get(ctx, "shoppinglist/invalid"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected invalid key removed, got err=%v", err)
	}
}

type prefixedListCache struct {
	*cache.FileCache
}

func (c prefixedListCache) List(ctx context.Context, prefix string, token string) ([]string, error) {
	keys, err := c.FileCache.List(ctx, prefix, token)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, prefix+key)
	}
	return out, nil
}
