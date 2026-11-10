package embeddings

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEmbedder struct {
	version string
	calls   [][]string
	err     error
}

func (f *fakeEmbedder) CacheVersion() string { return f.version }
func (f *fakeEmbedder) EmbedIngredients(_ context.Context, texts []string) ([]ai.IngredientEmbedding, error) {
	f.calls = append(f.calls, append([]string(nil), texts...))
	if f.err != nil {
		return nil, f.err
	}
	vectors := make([]ai.IngredientEmbedding, len(texts))
	for i := range vectors {
		vectors[i] = ai.IngredientEmbedding{1, 0}
	}
	return vectors, nil
}

func TestCacheIndependentOfGradesAndProductMetadata(t *testing.T) {
	c := cache.NewInMemoryCache()
	backend := &fakeEmbedder{version: "small/256/description-v1"}
	service := New(c, backend)
	inputs := []ai.InputIngredient{{ProductID: "a", Description: " Broccoli ", Grade: &ai.IngredientGrade{Score: 8}}, {ProductID: "b", Description: "Broccoli"}}
	got, err := service.EmbedIngredients(t.Context(), inputs)
	require.NoError(t, err)
	require.Len(t, backend.calls, 1)
	assert.Equal(t, []string{"Broccoli"}, backend.calls[0])
	assert.Equal(t, got[0].Embedding, got[1].Embedding)
	assert.Nil(t, inputs[0].Embedding)
	inputs[0].Grade = &ai.IngredientGrade{Score: 9, Reason: "new grader"}
	inputs[0].ProductID = "different-store-product"
	inputs[0].Brand = "new brand"
	got, err = service.EmbedIngredients(t.Context(), inputs)
	require.NoError(t, err)
	require.Len(t, backend.calls, 1)
	assert.Equal(t, 9, got[0].Grade.Score)
	assert.Equal(t, "new brand", got[0].Brand)
	assert.NotEmpty(t, got[0].Embedding)
}

func TestCacheChangesWithEmbeddingConfigurationOrDescription(t *testing.T) {
	c := cache.NewInMemoryCache()
	input := []ai.InputIngredient{{Description: "Broccoli", Embedding: ai.IngredientEmbedding{0, 1}}}
	for _, version := range []string{"small/256/description-v1", "large/256/description-v1", "small/512/description-v1", "small/256/description-v2"} {
		backend := &fakeEmbedder{version: version}
		got, err := New(c, backend).EmbedIngredients(t.Context(), input)
		require.NoError(t, err)
		require.Len(t, backend.calls, 1)
		assert.Equal(t, ai.IngredientEmbedding{1, 0}, got[0].Embedding)
	}
	backend := &fakeEmbedder{version: "small/256/description-v1"}
	_, err := New(c, backend).EmbedIngredients(t.Context(), []ai.InputIngredient{{Description: "Asparagus"}})
	require.NoError(t, err)
	require.Len(t, backend.calls, 1)
}

func TestEmbeddingFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		c           cache.Cache
		backend     *fakeEmbedder
		description string
		want        string
	}{
		{"api", cache.NewInMemoryCache(), &fakeEmbedder{version: "test", err: fmt.Errorf("API failed")}, "Broccoli", "API failed"},
		{"empty", cache.NewInMemoryCache(), &fakeEmbedder{version: "test"}, " ", "empty description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.c, tc.backend).EmbedIngredients(t.Context(), []ai.InputIngredient{{Description: tc.description}})
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, got)
		})
	}
}

// Every lookup must start before any can complete, proving reads overlap.
type overlappingReadCache struct {
	cache.Cache
	started chan string
	release chan struct{}
}

func (c overlappingReadCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	c.started <- key
	select {
	case <-c.release:
		return c.Cache.Get(ctx, key)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestCacheLookupsRunConcurrentlyAndPreserveOrder(t *testing.T) {
	backing := cache.NewInMemoryCache()
	require.NoError(t, backing.Put(t.Context(), cacheKey("test", "Broccoli"), `[0,1]`, cache.Unconditional()))
	c := overlappingReadCache{Cache: backing, started: make(chan string, 2), release: make(chan struct{})}
	backend := &fakeEmbedder{version: "test"}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	type outcome struct {
		ingredients []ai.InputIngredient
		err         error
	}
	done := make(chan outcome, 1)
	go func() {
		got, err := New(c, backend).EmbedIngredients(ctx, []ai.InputIngredient{{ProductID: "a", Description: "Broccoli"}, {ProductID: "b", Description: "Asparagus"}, {ProductID: "c", Description: "Broccoli"}})
		done <- outcome{got, err}
	}()
	for range 2 {
		select {
		case <-c.started:
		case <-ctx.Done():
			t.Fatal("cache lookups did not overlap")
		}
	}
	close(c.release)
	result := <-done
	require.NoError(t, result.err)
	require.Len(t, result.ingredients, 3)
	assert.Equal(t, ai.IngredientEmbedding{0, 1}, result.ingredients[0].Embedding)
	assert.Equal(t, ai.IngredientEmbedding{1, 0}, result.ingredients[1].Embedding)
	assert.Equal(t, ai.IngredientEmbedding{0, 1}, result.ingredients[2].Embedding)
	assert.Equal(t, [][]string{{"Asparagus"}}, backend.calls)
	assert.Empty(t, c.started)
}

func TestCacheLookupDecodeFailureReturnsNoPartialResults(t *testing.T) {
	c := cache.NewInMemoryCache()
	require.NoError(t, c.Put(t.Context(), cacheKey("test", "Broccoli"), `invalid JSON`, cache.Unconditional()))
	backend := &fakeEmbedder{version: "test"}
	got, err := New(c, backend).EmbedIngredients(t.Context(), []ai.InputIngredient{{Description: "Broccoli"}, {Description: "Asparagus"}})
	require.ErrorContains(t, err, `decode ingredient embedding "Broccoli"`)
	assert.Nil(t, got)
	assert.Empty(t, backend.calls)
}
