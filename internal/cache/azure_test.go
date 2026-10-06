package cache

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureCacheDoesNotSelectCockroach(t *testing.T) {
	t.Setenv("COCKROACH_DATABASE_URL", "invalid database URL")
	t.Setenv("AZURE_STORAGE_ACCOUNT_NAME", "caremetest")
	t.Setenv("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY", "dGVzdA==")
	for _, container := range []string{"recipes", "users", "recipe-images"} {
		t.Run(container, func(t *testing.T) {
			c, err := EnsureCache(container)
			require.NoError(t, err)
			require.IsType(t, &BlobCache{}, c)
		})
	}
}

func TestEnsureCacheRecipeImagesUseFiles(t *testing.T) {
	t.Setenv("COCKROACH_DATABASE_URL", "invalid database URL")
	t.Setenv("AZURE_STORAGE_ACCOUNT_NAME", "unused")
	require.NoError(t, os.Unsetenv("AZURE_STORAGE_ACCOUNT_NAME"))
	c, err := EnsureCache("recipe-images")
	require.NoError(t, err)
	fc := c.(*FileCache)
	fc.Dir = t.TempDir()
	require.NoError(t, fc.Put(t.Context(), "image", string([]byte{0, 255}), Unconditional()))
	r, err := fc.Get(t.Context(), "image")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.Equal(t, []byte{0, 255}, got)
}
