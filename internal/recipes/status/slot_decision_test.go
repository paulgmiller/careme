package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func slotStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	c := cache.NewInMemoryCache()
	owner, replica := NewStore(c), NewStore(c)
	require.NoError(t, owner.Start(t.Context(), "run", ""))
	require.NoError(t, owner.Plan(t.Context(), "run", []ai.RecipePlan{{Cuisine: "French"}}))
	return owner, replica
}

func TestSlotReplacementWinsAndAdvances(t *testing.T) {
	t.Parallel()
	owner, replica := slotStores(t)
	require.NoError(t, owner.RecipeDraft(t.Context(), "run", 0, "old"))
	require.NoError(t, replica.RequestReplacement(t.Context(), "run", 0, 0))
	require.NoError(t, replica.RequestReplacement(t.Context(), "run", 0, 0))
	replacing, err := owner.Decision(t.Context(), "run", 0, 0)
	require.NoError(t, err)
	require.True(t, replacing)
	ready, err := owner.FinishSlot(t.Context(), "run", 0, 0, "old")
	require.NoError(t, err)
	require.False(t, ready)
	got, err := replica.Load(t.Context(), "run")
	require.NoError(t, err)
	assert.Empty(t, got.Slots[0].RecipeHash)
	assert.False(t, got.Slots[0].Reviewed)
	assert.True(t, got.Slots[0].Replacing)
	// The overlay must not mutate the owner's stored payload.
	raw, err := owner.load(t.Context(), "run")
	require.NoError(t, err)
	assert.Equal(t, "old", raw.Slots[0].RecipeHash)
	for revision := 1; revision <= 3; revision++ {
		plan := ai.RecipePlan{Cuisine: fmt.Sprintf("Cuisine %d", revision)}
		require.NoError(t, owner.ReplaceSlot(t.Context(), "run", 0, revision, plan))
		got, err = replica.Load(t.Context(), "run")
		require.NoError(t, err)
		assert.Equal(t, Slot{Plan: plan, Revision: revision}, got.Slots[0])
		require.ErrorIs(t, replica.RequestReplacement(t.Context(), "run", 0, revision-1), ErrInvalidSlot)
		require.NoError(t, replica.RequestReplacement(t.Context(), "run", 0, revision))
	}
}

func TestSlotReadyWinsBeforePublication(t *testing.T) {
	t.Parallel()
	owner, replica := slotStores(t)
	ready, err := owner.FinishSlot(t.Context(), "run", 0, 0, "final")
	require.NoError(t, err)
	require.True(t, ready)
	ready, err = owner.FinishSlot(t.Context(), "run", 0, 0, "final")
	require.NoError(t, err)
	require.True(t, ready)
	require.ErrorIs(t, replica.RequestReplacement(t.Context(), "run", 0, 0), ErrSlotCompleted)
	got, err := replica.Load(t.Context(), "run")
	require.NoError(t, err)
	assert.Empty(t, got.Slots[0].RecipeHash)
	assert.False(t, got.Slots[0].Reviewed)
	replacing, err := replica.Decision(t.Context(), "run", 0, 0)
	require.NoError(t, err)
	assert.False(t, replacing)
	require.NoError(t, owner.RecipeReady(t.Context(), "run", 0, "final"))
	require.ErrorIs(t, replica.RequestReplacement(t.Context(), "run", 0, 0), ErrSlotCompleted)
}

func TestSlotDecisionCrossReplicaRace(t *testing.T) {
	t.Parallel()
	for range 100 {
		owner, replica := slotStores(t)
		start := make(chan struct{})
		var ready bool
		var readyErr, replaceErr error
		var wg sync.WaitGroup
		wg.Go(func() { <-start; ready, readyErr = owner.FinishSlot(t.Context(), "run", 0, 0, "final") })
		wg.Go(func() { <-start; replaceErr = replica.RequestReplacement(t.Context(), "run", 0, 0) })
		close(start)
		wg.Wait()
		require.NoError(t, readyErr)
		if ready {
			require.ErrorIs(t, replaceErr, ErrSlotCompleted)
		} else {
			require.NoError(t, replaceErr)
		}
		replacing, err := replica.Decision(t.Context(), "run", 0, 0)
		require.NoError(t, err)
		assert.Equal(t, !ready, replacing)
	}
}

func TestSlotReplacementValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		mutate          func(*testing.T, *Store)
		index, revision int
		want            error
	}{
		{name: "negative index", index: -1, want: ErrInvalidSlot},
		{name: "outside plan", index: 1, want: ErrInvalidSlot},
		{name: "stale revision", revision: 1, want: ErrInvalidSlot},
		{name: "reviewed", mutate: func(t *testing.T, s *Store) { require.NoError(t, s.RecipeReady(t.Context(), "run", 0, "ready")) }, want: ErrSlotCompleted},
		{name: "completed", mutate: func(t *testing.T, s *Store) { require.NoError(t, s.Complete(t.Context(), "run", "result")) }, want: ErrSlotCompleted},
		{name: "failed", mutate: func(t *testing.T, s *Store) { require.NoError(t, s.Fail(t.Context(), "run", errors.New("failed"))) }},
		{name: "timeout", mutate: func(t *testing.T, s *Store) {
			stored, err := s.load(t.Context(), "run")
			require.NoError(t, err)
			stored.StartedAt = time.Now().Add(-11 * time.Minute)
			require.NoError(t, s.save(t.Context(), "run", stored))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, replica := slotStores(t)
			if tc.mutate != nil {
				tc.mutate(t, owner)
			}
			err := replica.RequestReplacement(t.Context(), "run", tc.index, tc.revision)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.Error(t, err)
			}
			requested, err := owner.Decision(t.Context(), "run", 0, 0)
			require.NoError(t, err)
			assert.False(t, requested)
		})
	}
}

func TestReplaceSlotRequiresWinningRequest(t *testing.T) {
	t.Parallel()
	owner, _ := slotStores(t)
	require.Error(t, owner.ReplaceSlot(t.Context(), "run", 0, 1, ai.RecipePlan{}))
	_, err := owner.FinishSlot(t.Context(), "run", 0, 0, "")
	require.Error(t, err)
}

func TestRestartIsolatesSlotDecisions(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ready", "replace"} {
		t.Run(kind, func(t *testing.T) {
			owner, replica := slotStores(t)
			if kind == "ready" {
				ready, err := owner.FinishSlot(t.Context(), "run", 0, 0, "old")
				require.NoError(t, err)
				require.True(t, ready)
			} else {
				require.NoError(t, replica.RequestReplacement(t.Context(), "run", 0, 0))
			}
			owner.now = func() time.Time { return time.Now().Add(time.Minute) }
			require.NoError(t, owner.Start(t.Context(), "run", "new run"))
			require.NoError(t, owner.Plan(t.Context(), "run", []ai.RecipePlan{{Cuisine: "new"}}))
			requested, err := replica.Decision(t.Context(), "run", 0, 0)
			require.NoError(t, err)
			assert.False(t, requested)
			got, err := replica.Load(t.Context(), "run")
			require.NoError(t, err)
			assert.False(t, got.Slots[0].Replacing)
			require.NoError(t, replica.RequestReplacement(t.Context(), "run", 0, 0))
		})
	}
}

// Independent Store mutexes must not be needed to arbitrate a shared file cache.
func TestSlotDecisionSharedFileCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	owner := NewStore(cache.NewFileCache(dir))
	replica := NewStore(cache.NewFileCache(dir))
	require.NoError(t, owner.Start(t.Context(), "file-run", ""))
	require.NoError(t, owner.Plan(t.Context(), "file-run", []ai.RecipePlan{{}}))
	require.NoError(t, replica.RequestReplacement(t.Context(), "file-run", 0, 0))
	ready, err := owner.FinishSlot(t.Context(), "file-run", 0, 0, "final")
	require.NoError(t, err)
	assert.False(t, ready)
	got, err := replica.Load(t.Context(), "file-run")
	require.NoError(t, err)
	assert.True(t, got.Slots[0].Replacing)
}

type failingDecisionCache struct {
	cache.Cache
	getErr error
	putErr error
}

func (c failingDecisionCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.HasPrefix(key, slotDecisionCachePrefix) && c.getErr != nil {
		return nil, c.getErr
	}
	return c.Cache.Get(ctx, key)
}

func (c failingDecisionCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if strings.HasPrefix(key, slotDecisionCachePrefix) && c.putErr != nil {
		return c.putErr
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestSlotDecisionFailuresPropagate(t *testing.T) {
	t.Parallel()
	owner, _ := slotStores(t)
	failure := errors.New("storage unavailable")
	reader := NewStore(failingDecisionCache{Cache: owner.cache, getErr: failure})
	_, err := reader.Load(t.Context(), "run")
	require.ErrorIs(t, err, failure)
	_, err = reader.Decision(t.Context(), "run", 0, 0)
	require.ErrorIs(t, err, failure)
	writer := NewStore(failingDecisionCache{Cache: owner.cache, putErr: failure})
	require.ErrorIs(t, writer.RequestReplacement(t.Context(), "run", 0, 0), failure)
	_, err = writer.FinishSlot(t.Context(), "run", 0, 0, "final")
	require.ErrorIs(t, err, failure)
	stored, err := owner.load(t.Context(), "run")
	require.NoError(t, err)
	key, err := decisionKey("run", stored.StartedAt, 0, 0)
	require.NoError(t, err)
	require.NoError(t, owner.cache.Put(t.Context(), key, "bad", cache.Unconditional()))
	_, err = owner.Decision(t.Context(), "run", 0, 0)
	require.ErrorContains(t, err, "decode slot decision")
	require.NoError(t, owner.cache.Put(t.Context(), key, `{"kind":"unknown"}`, cache.Unconditional()))
	_, err = owner.Load(t.Context(), "run")
	require.ErrorContains(t, err, "invalid slot decision")
}
