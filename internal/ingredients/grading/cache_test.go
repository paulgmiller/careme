package grading

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type limitedGradeCache struct {
	cache.Cache
	active   atomic.Int32
	overflow atomic.Int32
	reads    atomic.Int32
	writes   atomic.Int32
	readErr  error
	writeErr error
}

func (c *limitedGradeCache) start() func() {
	if c.active.Add(1) > ingredientGradeCacheConcurrency {
		c.overflow.Add(1)
	}
	// Model storage latency so requests from concurrent catalogs overlap.
	time.Sleep(time.Millisecond)
	return func() { c.active.Add(-1) }
}

func (c *limitedGradeCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	defer c.start()()
	c.reads.Add(1)
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.Cache.Get(ctx, key)
}

func (c *limitedGradeCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	defer c.start()()
	c.writes.Add(1)
	if c.writeErr != nil {
		return c.writeErr
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestCachingGraderBoundsCacheTrafficAcrossConcurrentCatalogs(t *testing.T) {
	c := &limitedGradeCache{Cache: cache.NewInMemoryCache()}
	g := newCachingGrader(&stubGradeBackend{}, NewStore(c))
	done := make(chan error, 3)
	for catalog := range 3 {
		go func() {
			inputs := make([]ai.InputIngredient, 100)
			for i := range inputs {
				inputs[i].Description = fmt.Sprintf("catalog %d ingredient %d", catalog, i)
			}
			got, err := g.GradeIngredients(t.Context(), inputs)
			if err == nil && len(got) != len(inputs) {
				err = fmt.Errorf("got %d ingredients, want %d", len(got), len(inputs))
			}
			done <- err
		}()
	}
	for range 3 {
		require.NoError(t, <-done)
	}
	assert.Zero(t, c.overflow.Load(), "reads and writes must share a limit across catalogs")
	assert.EqualValues(t, 300, c.reads.Load())
	assert.EqualValues(t, 300, c.writes.Load())
}

func TestCachingGraderCanceledWhileWaitingForCache(t *testing.T) {
	c := &limitedGradeCache{Cache: cache.NewInMemoryCache()}
	backend := &stubGradeBackend{}
	g := newCachingGrader(backend, NewStore(c))
	for range ingredientGradeCacheConcurrency {
		g.cacheSlots <- struct{}{}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := g.GradeIngredients(ctx, []ai.InputIngredient{{Description: "Carrot"}})
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, c.reads.Load())
	assert.Empty(t, backend.calls)
}

func TestCachingGraderCacheFailureBehavior(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	for _, read := range []bool{true, false} {
		t.Run(fmt.Sprintf("read=%v", read), func(t *testing.T) {
			c := &limitedGradeCache{Cache: cache.NewInMemoryCache()}
			if read {
				c.readErr = storageErr
			} else {
				c.writeErr = storageErr
			}
			backend := &stubGradeBackend{}
			g := newCachingGrader(backend, NewStore(c))
			got, err := g.GradeIngredients(t.Context(), []ai.InputIngredient{{Description: "Carrot"}})
			if read {
				require.ErrorIs(t, err, storageErr)
				assert.Empty(t, got)
				assert.Empty(t, backend.calls)
			} else {
				require.NoError(t, err)
				require.Len(t, got, 1)
				assert.NotNil(t, got[0].Grade)
			}
			assert.Empty(t, g.cacheSlots, "cache slots must be released after failures")
		})
	}
}
