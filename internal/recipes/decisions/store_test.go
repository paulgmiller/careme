package decisions

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"careme/internal/cache"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecentDecisions(t *testing.T) {
	t.Parallel()
	store := NewStore(cache.NewInMemoryCache())
	// A non-UTC clock also exercises both ends of the UTC day partitions.
	now := time.Date(2026, 11, 19, 16, 30, 0, 0, time.FixedZone("PST", -8*3600))
	for _, tc := range []struct {
		hash string
		age  time.Duration
	}{
		{"recent", time.Hour},
		{"just-inside", Window - time.Nanosecond},
		{"boundary", Window},
		{"expired", Window + time.Nanosecond},
		{"old", 30 * 24 * time.Hour},
	} {
		require.NoError(t, store.Record(t.Context(), "user", utypes.Recipe{Title: tc.hash, Hash: tc.hash, CreatedAt: now.Add(-tc.age)}))
	}
	require.NoError(t, store.Record(t.Context(), "other", utypes.Recipe{Title: "Other user", Hash: "other", CreatedAt: now}))
	got, err := store.Recent(t.Context(), "user", now)
	require.NoError(t, err)
	titles := make([]string, 0, len(got))
	for _, r := range got {
		titles = append(titles, r.Title)
	}
	assert.ElementsMatch(t, []string{"recent", "just-inside"}, titles)

	// Repeating an action refreshes its cooldown, including across day boundaries.
	require.NoError(t, store.Record(t.Context(), "user", utypes.Recipe{Title: "old", Hash: "old", CreatedAt: now}))
	got, err = store.Recent(t.Context(), "user", now.Add(Window-time.Second))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "old", got[0].Hash)
}

func TestIndependentDecisionsDoNotOverwrite(t *testing.T) {
	t.Parallel()
	store := NewStore(cache.NewInMemoryCache())
	now := time.Now()
	var wg sync.WaitGroup
	for _, hash := range []string{"pasta", "curry"} {
		wg.Go(func() {
			assert.NoError(t, store.Record(t.Context(), "user", utypes.Recipe{Title: hash, Hash: hash, CreatedAt: now}))
		})
	}
	wg.Wait()
	got, err := store.Recent(t.Context(), "user", now)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

type failingCache struct {
	cache.ListCache
	operation string
}

var errStorage = errors.New("storage unavailable")

func (c failingCache) List(ctx context.Context, prefix, token string) ([]string, error) {
	if c.operation == "list" {
		return nil, errStorage
	}
	return c.ListCache.List(ctx, prefix, token)
}

func (c failingCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if c.operation == "get" {
		return nil, errStorage
	}
	return c.ListCache.Get(ctx, key)
}

func (c failingCache) Put(ctx context.Context, key, body string, opts cache.PutOptions) error {
	if c.operation == "put" {
		return errStorage
	}
	return c.ListCache.Put(ctx, key, body, opts)
}

func TestStorageFailures(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, operation := range []string{"list", "get", "put", "decode"} {
		t.Run(operation, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			store := NewStore(c)
			recipe := utypes.Recipe{Title: "Pasta", Hash: "pasta", CreatedAt: now}
			require.NoError(t, store.Record(t.Context(), "user", recipe))
			store = NewStore(failingCache{c, operation})
			if operation == "put" {
				assert.ErrorIs(t, store.Record(t.Context(), "user", recipe), errStorage)
				return
			}
			if operation == "decode" {
				require.NoError(t, c.Put(t.Context(), dayPrefix("user", now)+"pasta", "invalid JSON", cache.Unconditional()))
			}
			got, err := store.Recent(t.Context(), "user", now)
			require.Error(t, err)
			assert.Nil(t, got, "required history must not be partially returned")
		})
	}
}
