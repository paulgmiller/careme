package status

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadTimeout(t *testing.T) {
	p := Payload{StartedAt: time.Now().Add(-recipeGenerationTimeout - time.Minute)}
	assert.Equal(t, "Recipe generation timed out.", p.Failed())
}

func TestPayloadFailed(t *testing.T) {
	p := Payload{Error: "Kaboom", StartedAt: time.Now()}
	assert.Equal(t, "Kaboom", p.Failed())
	assert.Equal(t, Payload{StartedAt: time.Now()}.Failed(), "")
}

func TestPayloadCompletedIsNotFailedAfterTimeout(t *testing.T) {
	p := Payload{
		StartedAt: time.Now().Add(-recipeGenerationTimeout - time.Minute),
		Error:     "stale failure",
		Redirect:  "new-hash",
	}

	assert.Empty(t, p.Failed())
}

func TestGenerationStatusProgressPreservesStartAndRestartClearsError(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	startedAt := time.Date(2026, 8, 25, 12, 30, 0, 0, time.FixedZone("PDT", -7*60*60))
	statuses.now = func() time.Time { return startedAt }

	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	require.NoError(t, statuses.Update(t.Context(), "status-lifecycle", "Gathering ingredients"))

	got, err := statuses.Load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "Gathering ingredients", got.Message)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)
	assert.Empty(t, got.Error)

	require.NoError(t, statuses.Fail(t.Context(), "status-lifecycle", errors.New("store returned 404")))
	got, err = statuses.Load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "store returned 404", got.Error)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)

	retriedAt := startedAt.Add(time.Minute)
	statuses.now = func() time.Time { return retriedAt }
	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	got, err = statuses.Load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.Empty(t, got.Message)
	assert.Equal(t, retriedAt.UTC(), got.StartedAt)
}

func TestUpdateKeepsFiveRecentLines(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash))

	require.NoError(t, statuses.Update(t.Context(), hash, "one\ntwo\n"))
	require.NoError(t, statuses.Update(t.Context(), hash, "three\nfour\nfive"))

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "three\nfour\nfive\none\ntwo", got.Message)

	require.NoError(t, statuses.Update(t.Context(), hash, "six\nseven"))
	got, err = statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "six\nseven\nthree\nfour\nfive", got.Message)
}

func TestUpdateCapsFirstStatusAtFiveLines(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash))

	require.NoError(t, statuses.Update(t.Context(), hash, "one\ntwo\nthree\nfour\nfive\nsix"))

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\nfour\nfive", got.Message)
}

func TestUpdateKeepsConcurrentLines(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-concurrent"
	require.NoError(t, statuses.Start(t.Context(), hash))

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, status := range []string{"one", "two", "three"} {
		wg.Add(1)
		go func(status string) {
			defer wg.Done()
			errs <- statuses.Update(t.Context(), hash, status)
		}(status)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"one", "two", "three"}, strings.Split(got.Message, "\n"))
}

func TestGenerationStatusFailRecordsTerminalError(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "failed"))

	require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

	got, err := statuses.Load(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "plan exploded", got.Error)
	assert.Empty(t, got.Message)
}

func TestGenerationStatusTerminalStatesAreExclusive(t *testing.T) {
	t.Run("completed generation cannot fail", func(t *testing.T) {
		statuses := NewStore(cache.NewInMemoryCache())
		require.NoError(t, statuses.Start(t.Context(), "completed"))
		require.NoError(t, statuses.Complete(t.Context(), "completed", "new-hash"))

		err := statuses.Fail(t.Context(), "completed", errors.New("late failure"))
		require.ErrorContains(t, err, "already completed")

		got, loadErr := statuses.Load(t.Context(), "completed")
		require.NoError(t, loadErr)
		assert.Equal(t, "new-hash", got.NewHash())
		assert.Empty(t, got.Failed())
	})

	t.Run("failed generation cannot complete", func(t *testing.T) {
		statuses := NewStore(cache.NewInMemoryCache())
		require.NoError(t, statuses.Start(t.Context(), "failed"))
		require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

		err := statuses.Complete(t.Context(), "failed", "new-hash")
		require.ErrorContains(t, err, "already failed")

		got, loadErr := statuses.Load(t.Context(), "failed")
		require.NoError(t, loadErr)
		assert.Empty(t, got.NewHash())
		assert.Equal(t, "plan exploded", got.Failed())
	})
}

func TestGenerationStatusCompleteRequiresHash(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "running"))

	err := statuses.Complete(t.Context(), "running", "  ")
	require.ErrorContains(t, err, "completed generation hash is required")
}

func TestGenerationStatusDecodesLegacyText(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	statuses := NewStore(cacheStore)
	startedAt := time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC)
	statuses.now = func() time.Time { return startedAt }
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"running", "Still chopping", cache.Unconditional()))
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"failed", "Something went wrong: store returned 404", cache.Unconditional()))

	running, err := statuses.Load(t.Context(), "running")
	require.NoError(t, err)
	assert.Equal(t, "Still chopping", running.Message)
	assert.Empty(t, running.Error)
	assert.Equal(t, startedAt, running.StartedAt)

	failed, err := statuses.Load(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "Something went wrong: store returned 404", failed.Message)
}
