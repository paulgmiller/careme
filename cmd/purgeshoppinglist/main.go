package main

import (
	"careme/internal/cache"
	"careme/internal/recipes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type purgeCache interface {
	cache.ListCache
	Delete(ctx context.Context, key string) error
}

type purgeStats struct {
	Found        int
	Valid        int
	Invalid      int
	WouldDelete  int
	Deleted      int
	DeleteErrors int
}

func main() {
	var apply bool
	flag.BoolVar(&apply, "apply", false, "Delete invalid shopping lists. Default is dry-run.")
	flag.Parse()

	ctx := context.Background()
	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	purgeable, ok := cacheStore.(purgeCache)
	if !ok {
		log.Fatalf("cache implementation %T does not support delete", cacheStore)
	}

	stats, err := purgeInvalidShoppingLists(ctx, purgeable, apply, os.Stdout)
	if err != nil {
		log.Fatalf("purge failed: %v", err)
	}

	fmt.Printf(
		"done: found=%d valid=%d invalid=%d would_delete=%d deleted=%d delete_errors=%d mode=%s\n",
		stats.Found,
		stats.Valid,
		stats.Invalid,
		stats.WouldDelete,
		stats.Deleted,
		stats.DeleteErrors,
		mode(apply),
	)
}

func purgeInvalidShoppingLists(ctx context.Context, c purgeCache, apply bool, out io.Writer) (purgeStats, error) {
	var stats purgeStats

	keys, err := c.List(ctx, recipes.ShoppingListCachePrefix, "")
	if err != nil {
		return stats, fmt.Errorf("list shopping lists: %w", err)
	}

	hashes := normalizeShoppingListHashes(keys)
	stats.Found = len(hashes)

	rio := recipes.IO(c)
	for _, hash := range hashes {
		_, err := rio.FromCache(ctx, hash)
		if err == nil {
			stats.Valid++
			continue
		}

		stats.Invalid++
		key := recipes.ShoppingListCachePrefix + hash
		if !apply {
			stats.WouldDelete++
			_, _ = fmt.Fprintf(out, "would delete %s (failed to load: %v)\n", key, err)
			continue
		}

		if err := c.Delete(ctx, key); err != nil {
			stats.DeleteErrors++
			_, _ = fmt.Fprintf(out, "failed delete %s: %v\n", key, err)
			continue
		}

		stats.Deleted++
		_, _ = fmt.Fprintf(out, "deleted %s (failed to load: %v)\n", key, err)
	}

	return stats, nil
}

func normalizeShoppingListHashes(keys []string) []string {
	hashes := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))

	for _, key := range keys {
		hash := filepath.ToSlash(strings.TrimSpace(key))
		hash = strings.TrimPrefix(hash, "./")
		hash = strings.TrimPrefix(hash, "/")
		hash = strings.TrimPrefix(hash, recipes.ShoppingListCachePrefix)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}

	slices.Sort(hashes)
	return hashes
}

func mode(apply bool) string {
	if apply {
		return "apply"
	}
	return "dry-run"
}
