package recipes

import (
	"strings"
	"sync"
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveGenerationStatusKeepsFiveRecentLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-tail"

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "one\ntwo\n"))
	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "three\nfour\nfive"))

	got, err := statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "three\nfour\nfive\none\ntwo", got)

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "six\nseven"))
	got, err = statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "six\nseven\nthree\nfour\nfive", got)
}

func TestSaveGenerationStatusCapsFirstStatusAtFiveLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-tail"

	require.NoError(t, statuses.SaveGenerationStatus(t.Context(), hash, "one\ntwo\nthree\nfour\nfive\nsix"))

	got, err := statuses.GenerationStatusFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\nfour\nfive", got)
}

func TestSaveGenerationStatusKeepsConcurrentLines(t *testing.T) {
	statuses := StatusStore(cache.NewInMemoryCache())
	hash := "status-concurrent"

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
	assert.ElementsMatch(t, []string{"one", "two", "three"}, strings.Split(got, "\n"))
}
