package main

import (
	"bytes"
	"careme/internal/cache"
	"context"
	"encoding/base64"
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

	"github.com/samber/lo"
)

const shoppingListPrefix = "shoppinglist/"
const paramsPrefix = "params/"
const ingredientsPrefix = "ingredients/"
const legacyRecipeHashSeed = "recipe"
const legacyIngredientsHashSeed = "ingredients"

type migrationStats struct {
	RootKeys           int
	ShoppingListKeys   int
	ParamsKeys         int
	IngredientsKeys    int
	Copied             int
	SkippedExisting    int
	SkippedUnsupported int
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
		"done: root=%d shoppinglists=%d params=%d ingredients=%d copied=%d skipped_existing=%d skipped_unsupported=%d mode=%s\n",
		stats.RootKeys,
		stats.ShoppingListKeys,
		stats.ParamsKeys,
		stats.IngredientsKeys,
		stats.Copied,
		stats.SkippedExisting,
		stats.SkippedUnsupported,
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

	for _, key := range rootKeys {
		switch {
		case strings.HasSuffix(key, ".params"):
			stats.ParamsKeys++
			recipeHash := strings.TrimSuffix(key, ".params")
			newKey := paramsPrefix + canonicalOrOriginalHash(recipeHash, legacyRecipeHashSeed)
			copied, skipped, err := copyKey(ctx, c, key, newKey, apply, out)
			if err != nil {
				return stats, err
			}
			stats.Copied += copied
			stats.SkippedExisting += skipped
			continue
		case hasLegacyHashSeed(key, legacyIngredientsHashSeed):
			stats.IngredientsKeys++
			newKey := ingredientsPrefix + canonicalOrOriginalHash(key, legacyIngredientsHashSeed)
			copied, skipped, err := copyKey(ctx, c, key, newKey, apply, out)
			if err != nil {
				return stats, err
			}
			stats.Copied += copied
			stats.SkippedExisting += skipped
			continue
		case hasLegacyHashSeed(key, legacyRecipeHashSeed):
			stats.ShoppingListKeys++
			newKey := shoppingListPrefix + canonicalOrOriginalHash(key, legacyRecipeHashSeed)
			copied, skipped, err := copyKey(ctx, c, key, newKey, apply, out)
			if err != nil {
				return stats, err
			}
			stats.Copied += copied
			stats.SkippedExisting += skipped
		default:
			stats.SkippedUnsupported++
			continue
		}
	}

	return stats, nil
}

func copyKey(ctx context.Context, c cache.Cache, srcKey, dstKey string, apply bool, out io.Writer) (copied int, skippedExisting int, err error) {
	exists, err := c.Exists(ctx, dstKey)
	if err != nil {
		return 0, 0, fmt.Errorf("check destination %q: %w", dstKey, err)
	}
	if exists {
		_ = lo.Must(fmt.Fprintf(out, "skip existing %s -> %s\n", srcKey, dstKey))
		return 0, 1, nil
	}

	if !apply {
		_ = lo.Must(fmt.Fprintf(out, "would copy %s -> %s\n", srcKey, dstKey))
		return 1, 0, nil
	}

	payload, err := readKey(ctx, c, srcKey)
	if err != nil {
		return 0, 0, fmt.Errorf("read %q: %w", srcKey, err)
	}
	if err := c.Put(ctx, dstKey, string(payload), cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			_ = lo.Must(fmt.Fprintf(out, "skip existing %s -> %s\n", srcKey, dstKey))
			return 0, 1, nil
		}
		return 0, 0, fmt.Errorf("write %q: %w", dstKey, err)
	}

	_ = lo.Must(fmt.Fprintf(out, "copied %s -> %s\n", srcKey, dstKey))
	return 1, 0, nil
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

func hasLegacyHashSeed(hash string, seed string) bool {
	decoded, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		return false
	}
	seedBytes := []byte(seed)
	return bytes.HasPrefix(decoded, seedBytes) && len(decoded) > len(seedBytes)
}

func canonicalOrOriginalHash(hash string, seed string) string {
	decoded, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		return hash
	}
	seedBytes := []byte(seed)
	if !bytes.HasPrefix(decoded, seedBytes) || len(decoded) == len(seedBytes) {
		return hash
	}
	return base64.RawURLEncoding.EncodeToString(decoded[len(seedBytes):])
}
