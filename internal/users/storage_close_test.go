package users

import (
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/require"
)

func TestStorageCloseDoesNotCloseBorrowedCache(t *testing.T) {
	c := &borrowedStorageCache{ListCache: cache.NewInMemoryCache()}
	s := NewStorage(c)
	require.NoError(t, s.Close())
	require.False(t, c.closed)
	require.NoError(t, c.Put(t.Context(), "key", `{}`, cache.Unconditional()))
	exists, err := c.Exists(t.Context(), "key")
	require.NoError(t, err)
	require.True(t, exists)
}

type borrowedStorageCache struct {
	cache.ListCache
	closed bool
}

func (c *borrowedStorageCache) Close() error {
	c.closed = true
	return nil
}

func TestNewCockroachStorageRequiresDatabaseURL(t *testing.T) {
	s, err := NewCockroachStorage(t.Context(), "")
	require.ErrorContains(t, err, "open user storage")
	require.Nil(t, s)
}
