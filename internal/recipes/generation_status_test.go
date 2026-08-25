package recipes

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

func TestGenerationStatusProgressPreservesStartAndRestartClearsError(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	startedAt := time.Date(2026, 8, 25, 12, 30, 0, 0, time.FixedZone("PDT", -7*60*60))
	statuses.now = func() time.Time { return startedAt }

	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), "status-lifecycle", "Gathering ingredients"))

	got, err := statuses.GenerationStatusFromCache(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "Gathering ingredients", got.Message)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)
	assert.Empty(t, got.Error)

	require.NoError(t, statuses.Fail(t.Context(), "status-lifecycle", errors.New("store returned 404")))
	got, err = statuses.GenerationStatusFromCache(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "store returned 404", got.Error)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)

	retriedAt := startedAt.Add(time.Minute)
	statuses.now = func() time.Time { return retriedAt }
	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle"))
	got, err = statuses.GenerationStatusFromCache(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.Empty(t, got.Message)
	assert.Equal(t, retriedAt.UTC(), got.StartedAt)
}

func TestSaveGenerationStatusKeepsFiveRecentLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash))

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "one\ntwo\n"))
	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "three\nfour\nfive"))

	got, err := statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "three\nfour\nfive\none\ntwo", got.Message)

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "six\nseven"))
	got, err = statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "six\nseven\nthree\nfour\nfive", got.Message)
}

func TestSaveGenerationStatusCapsFirstStatusAtFiveLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash))

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "one\ntwo\nthree\nfour\nfive\nsix"))

	got, err := statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\nfour\nfive", got.Message)
}

func TestSaveGenerationStatusKeepsConcurrentLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-concurrent"
	require.NoError(t, statuses.Start(t.Context(), hash))

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, status := range []string{"one", "two", "three"} {
		wg.Add(1)
		go func(status string) {
			defer wg.Done()
			errs <- statuses.SaveGenerationStatus(t.Context(), hash, status)
		}(status)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	got, err := statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"one", "two", "three"}, strings.Split(got.Message, "\n"))
}

func TestGenerationStatusFailRecordsTerminalError(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "failed"))

	require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

	got, err := statuses.GenerationStatusFromCache(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "plan exploded", got.Error)
	assert.Empty(t, got.Message)
}

func TestGenerationStatusDecodesLegacyText(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	statuses := StatusStore(cacheStore)
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"running", "Still chopping", cache.Unconditional()))
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"failed", "Something went wrong: store returned 404", cache.Unconditional()))

	running, err := statuses.GenerationStatusFromCache(t.Context(), "running")
	require.NoError(t, err)
	assert.Equal(t, "Still chopping", running.Message)
	assert.Empty(t, running.Error)
	assert.True(t, running.StartedAt.IsZero())

	failed, err := statuses.GenerationStatusFromCache(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "Something went wrong: store returned 404", failed.Message)
	assert.Equal(t, "store returned 404", failed.Error)
	assert.True(t, failed.StartedAt.IsZero())
}

func TestGenerationStatusDecodesStateBasedRecord(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	statuses := StatusStore(cacheStore)
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"failed", `{"state":"failed","message":"Something went wrong: store returned 404","started_at":"2026-08-25T12:30:00Z"}`, cache.Unconditional()))

	got, err := statuses.GenerationStatusFromCache(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "store returned 404", got.Error)
	assert.Equal(t, "Something went wrong: store returned 404", got.Message)
	assert.Equal(t, time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC), got.StartedAt)
}

func TestGenerationStatusRejectsInvalidStructuredState(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	statuses := StatusStore(cacheStore)
	require.NoError(t, cacheStore.Put(t.Context(), generationStatusCachePrefix+"invalid", `{"state":"mystery","started_at":"2026-08-25T12:30:00Z"}`, cache.Unconditional()))

	_, err := statuses.GenerationStatusFromCache(t.Context(), "invalid")
	require.Error(t, err)
}
