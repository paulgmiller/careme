package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/require"
)

func testStorage(account string) storage {
	return storage{account: account, recipes: cache.NewInMemoryCache(), images: cache.NewInMemoryCache()}
}

func seed(t *testing.T, s storage) {
	t.Helper()
	require.NoError(t, s.recipes.Put(t.Context(), "recipe/hash", `{"title":"Dinner","unknown_future_field":true}`, cache.Unconditional()))
	require.NoError(t, s.recipes.Put(t.Context(), "wine_recommendations/hash", `{"wines":[{"id":"wine-1","name":"Pinot Noir"}],"commentary":"A pairing"}`, cache.Unconditional()))
	require.NoError(t, s.images.Put(t.Context(), "recipes/hash", "RIFF\x00\xffWEBP", cache.Unconditional()))
}

func TestMigrate(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			source, destination := testStorage("test"), testStorage("prod")
			seed(t, source)
			for range 2 { // Repeating a successful migration is harmless.
				require.NoError(t, migrate(t.Context(), source, destination, "hash", apply, io.Discard))
			}
			for _, pair := range []struct {
				key string
				src cache.Cache
				dst cache.Cache
			}{
				{"recipe/hash", source.recipes, destination.recipes},
				{"wine_recommendations/hash", source.recipes, destination.recipes},
				{"recipes/hash", source.images, destination.images},
			} {
				want, err := readRecord(t.Context(), pair.src, pair.key)
				require.NoError(t, err)
				got, err := readRecord(t.Context(), pair.dst, pair.key)
				if apply {
					require.NoError(t, err)
					require.Equal(t, want, got)
				} else {
					require.ErrorIs(t, err, cache.ErrNotFound)
				}
			}
		})
	}
}

func TestMigratePreflight(t *testing.T) {
	for _, missing := range []string{"recipe/hash", "wine_recommendations/hash", "recipes/hash"} {
		t.Run(missing, func(t *testing.T) {
			source, destination := testStorage("test"), testStorage("prod")
			seed(t, source)
			source.recipes = &failingCache{Cache: source.recipes, missing: missing}
			source.images = &failingCache{Cache: source.images, missing: missing}
			require.ErrorIs(t, migrate(t.Context(), source, destination, "hash", true, io.Discard), cache.ErrNotFound)
			keys, err := destination.images.(cache.ListCache).List(t.Context(), "", "")
			require.NoError(t, err)
			require.Empty(t, keys)
			keys, err = destination.recipes.(cache.ListCache).List(t.Context(), "", "")
			require.NoError(t, err)
			require.Empty(t, keys)
		})
	}
	t.Run("conflict", func(t *testing.T) {
		source, destination := testStorage("test"), testStorage("prod")
		seed(t, source)
		require.NoError(t, destination.recipes.Put(t.Context(), "recipe/hash", "different", cache.Unconditional()))
		require.ErrorContains(t, migrate(t.Context(), source, destination, "hash", true, io.Discard), "conflict")
		got, err := readRecord(t.Context(), destination.recipes, "recipe/hash")
		require.NoError(t, err)
		require.Equal(t, "different", string(got))
		exists, err := destination.images.Exists(t.Context(), "recipes/hash")
		require.NoError(t, err)
		require.False(t, exists)
	})
}

type failingCache struct {
	cache.Cache
	missing string
	putErr  error
}

func (c *failingCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == c.missing {
		return nil, cache.ErrNotFound
	}
	return c.Cache.Get(ctx, key)
}

func (c *failingCache) PutReader(context.Context, string, io.Reader, cache.PutOptions) error {
	return c.putErr
}

func TestMigrateResumesAfterWriteFailure(t *testing.T) {
	source, destination := testStorage("test"), testStorage("prod")
	seed(t, source)
	recipeCache := destination.recipes
	destination.recipes = &failingCache{Cache: recipeCache, putErr: errors.New("write failed")}
	require.ErrorContains(t, migrate(t.Context(), source, destination, "hash", true, io.Discard), "write failed")
	exists, err := recipeCache.Exists(t.Context(), "recipe/hash")
	require.NoError(t, err)
	require.False(t, exists)
	destination.recipes = recipeCache
	require.NoError(t, migrate(t.Context(), source, destination, "hash", true, io.Discard))
}

func TestMigrationConfig(t *testing.T) {
	s := testStorage("test")
	require.ErrorContains(t, migrate(t.Context(), s, s, "hash", true, io.Discard), "must differ")
	for _, hash := range []string{"", "../hash", "a/b", "a\\b", "a b", ".", ".."} {
		require.Error(t, validateHash(hash))
	}
	t.Setenv("AZURE_STORAGE_ACCOUNT_NAME", "ambient-account")
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("AZURE_STORAGE_ACCOUNT_NAME=explicit\nAZURE_STORAGE_PRIMARY_ACCOUNT_KEY=a2V5\n"), 0o600))
	got, err := openStorage(path)
	require.NoError(t, err)
	require.Equal(t, "explicit", got.account)
	require.Equal(t, "ambient-account", os.Getenv("AZURE_STORAGE_ACCOUNT_NAME"))
	require.NoError(t, os.WriteFile(path, []byte("AZURE_STORAGE_ACCOUNT_NAME=explicit\n"), 0o600))
	_, err = openStorage(path)
	require.Error(t, err)
}
