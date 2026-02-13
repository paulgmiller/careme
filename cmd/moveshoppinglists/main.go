package main

import (
	"bytes"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const shoppingListPrefix = "shoppinglist/"

type deleter interface {
	Delete(ctx context.Context, key string) error
}

type migrationStats struct {
	RootKeys           int
	ShoppingListKeys   int
	Moved              int
	SkippedExisting    int
	SkippedNonShopping int
}

func main() {
	var apply bool
	flag.BoolVar(&apply, "apply", false, "Apply changes. Default is dry-run.")
	flag.Parse()

	ctx := context.Background()
	c, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	stats, err := migrateShoppingLists(ctx, c, apply, os.Stdout)
	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	fmt.Printf(
		"done: root=%d shoppinglists=%d moved=%d skipped_existing=%d skipped_non_shopping=%d mode=%s\n",
		stats.RootKeys,
		stats.ShoppingListKeys,
		stats.Moved,
		stats.SkippedExisting,
		stats.SkippedNonShopping,
		mode(apply),
	)
}

func mode(apply bool) string {
	if apply {
		return "apply"
	}
	return "dry-run"
}

func migrateShoppingLists(ctx context.Context, c cache.ListCache, apply bool, out io.Writer) (migrationStats, error) {
	var stats migrationStats

	rootKeys, err := listRootKeys(ctx, c)
	if err != nil {
		return stats, err
	}
	stats.RootKeys = len(rootKeys)

	var d deleter
	if apply {
		var ok bool
		d, ok = c.(deleter)
		if !ok {
			return stats, fmt.Errorf("cache backend %T does not support delete", c)
		}
	}

	for _, key := range rootKeys {
		payload, err := readKey(ctx, c, key)
		if err != nil {
			return stats, fmt.Errorf("read %q: %w", key, err)
		}
		if !isShoppingList(payload) {
			stats.SkippedNonShopping++
			continue
		}

		stats.ShoppingListKeys++
		newKey := shoppingListPrefix + key
		exists, err := c.Exists(ctx, newKey)
		if err != nil {
			return stats, fmt.Errorf("check destination %q: %w", newKey, err)
		}
		if exists {
			stats.SkippedExisting++
			fmt.Fprintf(out, "skip existing %s -> %s\n", key, newKey)
			continue
		}

		if !apply {
			stats.Moved++
			fmt.Fprintf(out, "would move %s -> %s\n", key, newKey)
			continue
		}

		if err := c.Put(ctx, newKey, string(payload), cache.IfNoneMatch()); err != nil {
			if errors.Is(err, cache.ErrAlreadyExists) {
				stats.SkippedExisting++
				fmt.Fprintf(out, "skip existing %s -> %s\n", key, newKey)
				continue
			}
			return stats, fmt.Errorf("write %q: %w", newKey, err)
		}
		if err := d.Delete(ctx, key); err != nil && !errors.Is(err, cache.ErrNotFound) {
			return stats, fmt.Errorf("delete %q: %w", key, err)
		}

		stats.Moved++
		fmt.Fprintf(out, "moved %s -> %s\n", key, newKey)
	}

	return stats, nil
}

func listRootKeys(ctx context.Context, c cache.ListCache) ([]string, error) {
	if fc, ok := c.(*cache.FileCache); ok {
		return listFileRootKeys(fc.Dir)
	}

	keys, err := c.List(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return normalizeRootKeys(keys), nil
}

func listFileRootKeys(dir string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	keys := make([]string, 0, 128)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if key == "." || strings.Contains(key, "/") {
			return nil
		}
		keys = append(keys, key)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(keys)
	return keys, nil
}

func normalizeRootKeys(keys []string) []string {
	root := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		k := filepath.ToSlash(strings.TrimSpace(key))
		k = strings.TrimPrefix(k, "./")
		k = strings.TrimPrefix(k, "/")
		if k == "" || strings.Contains(k, "/") {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		root = append(root, k)
	}
	slices.Sort(root)
	return root
}

func readKey(ctx context.Context, c cache.Cache, key string) ([]byte, error) {
	r, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = r.Close()
	}()
	return io.ReadAll(r)
}

func isShoppingList(payload []byte) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil {
		return false
	}

	recipesRaw, ok := obj["recipes"]
	if !ok {
		return false
	}
	if bytes.Equal(bytes.TrimSpace(recipesRaw), []byte("null")) {
		return false
	}

	var recipes []json.RawMessage
	return json.Unmarshal(recipesRaw, &recipes) == nil
}
