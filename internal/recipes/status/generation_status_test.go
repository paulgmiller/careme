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

func TestIDIsStableAndURLSafe(t *testing.T) {
	id := ID("old-hash", "response/id+with=padding")

	require.Len(t, id, 22)
	assert.NotContains(t, id, "+")
	assert.NotContains(t, id, "/")
	assert.NotContains(t, id, "=")
	assert.True(t, IsValidID(id))
	assert.Equal(t, id, ID("old-hash", "response/id+with=padding"))
	assert.NotEqual(t, id, ID("other-hash", "response/id+with=padding"))

	for _, invalid := range []string{"", "not-an-id", id + "=", " " + id} {
		assert.False(t, IsValidID(invalid), invalid)
	}
}

func TestPayloadTimeout(t *testing.T) {
	p := payload{StartedAt: time.Now().Add(-recipeGenerationTimeout - time.Minute)}
	assert.Equal(t, "Recipe generation timed out.", p.Failed())
}

func TestPayloadFailed(t *testing.T) {
	p := payload{Error: "Kaboom", StartedAt: time.Now()}
	assert.Equal(t, "Kaboom", p.Failed())
	assert.Equal(t, payload{StartedAt: time.Now()}.Failed(), "")
}

func TestPayloadCompletedIsNotFailedAfterTimeout(t *testing.T) {
	p := payload{
		StartedAt:    time.Now().Add(-recipeGenerationTimeout - time.Minute),
		Error:        "stale failure",
		RedirectHash: "new-hash",
	}

	assert.Empty(t, p.Failed())
}

func TestGenerationStatusProgressPreservesStartAndRestartClearsError(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	startedAt := time.Now().In(time.FixedZone("PDT", -7*60*60)).Truncate(time.Second)
	statuses.now = func() time.Time { return startedAt }

	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	require.NoError(t, statuses.Update(t.Context(), "status-lifecycle", "Gathering ingredients"))

	got, err := statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "Gathering ingredients", got.Message())
	assert.Equal(t, startedAt.UTC(), got.StartedAt)
	assert.Empty(t, got.Failed())

	require.NoError(t, statuses.Fail(t.Context(), "status-lifecycle", errors.New("store returned 404")))
	got, err = statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "store returned 404", got.Failed())
	assert.Equal(t, startedAt.UTC(), got.StartedAt)

	retriedAt := startedAt.Add(time.Minute)
	statuses.now = func() time.Time { return retriedAt }
	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	got, err = statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Empty(t, got.Failed())
	assert.Empty(t, got.Message())
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
	assert.Equal(t, "three\nfour\nfive\none\ntwo", got.Message())

	require.NoError(t, statuses.Update(t.Context(), hash, "six\nseven"))
	got, err = statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "six\nseven\nthree\nfour\nfive", got.Message())
}

func TestUpdateCapsFirstStatusAtFiveLines(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash))

	require.NoError(t, statuses.Update(t.Context(), hash, "one\ntwo\nthree\nfour\nfive\nsix"))

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\nfour\nfive", got.Message())
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
	assert.ElementsMatch(t, []string{"one", "two", "three"}, strings.Split(got.Message(), "\n"))
}

func TestGenerationStatusFailRecordsTerminalError(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "failed"))

	require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

	got, err := statuses.Load(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "plan exploded", got.Failed())
	assert.Empty(t, got.Message())
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
		assert.Equal(t, "new-hash", got.Redirect())
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
		assert.Empty(t, got.Redirect())
		assert.Equal(t, "plan exploded", got.Failed())
	})
}

func TestGenerationStatusCompleteRequiresHash(t *testing.T) {
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "running"))

	err := statuses.Complete(t.Context(), "running", "  ")
	require.ErrorContains(t, err, "completed generation hash is required")
}
