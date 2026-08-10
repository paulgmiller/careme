package regeneration

import (
	"errors"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDIsStableAndURLSafe(t *testing.T) {
	id := ID("old-hash", "response/id+with=padding")

	require.Len(t, id, 22)
	assert.False(t, strings.ContainsAny(id, "+/="))
	assert.Equal(t, id, ID("old-hash", "response/id+with=padding"))
	assert.NotEqual(t, id, ID("other-hash", "response/id+with=padding"))
}

func TestStoreLifecycle(t *testing.T) {
	store := NewStore(cache.NewFileCache(t.TempDir()), 10*time.Minute)
	id := ID("old-hash", "response-id")

	require.NoError(t, store.Start(t.Context(), id))
	newHash, timedOut, err := store.Load(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, newHash)
	assert.False(t, timedOut)

	err = store.Start(t.Context(), id)
	require.ErrorIs(t, err, cache.ErrAlreadyExists)

	require.NoError(t, store.Complete(t.Context(), id, "new-hash"))
	newHash, timedOut, err = store.Load(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, "new-hash", newHash)
	assert.False(t, timedOut)
}

func TestLoadReportsTimedOutRunningRegeneration(t *testing.T) {
	store := NewStore(cache.NewFileCache(t.TempDir()), time.Nanosecond)
	id := ID("old-hash", "response-id")
	require.NoError(t, store.Start(t.Context(), id))

	newHash, timedOut, err := store.Load(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, newHash)
	assert.True(t, timedOut)
}

func TestLoadRejectsInvalidID(t *testing.T) {
	store := NewStore(cache.NewFileCache(t.TempDir()), 10*time.Minute)

	_, _, err := store.Load(t.Context(), "not-an-id")
	require.Error(t, err)
	assert.True(t, errors.Is(err, cache.ErrNotFound))
}
