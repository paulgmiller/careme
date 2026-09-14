package embeddings

import (
	"context"
	"fmt"
	"testing"

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

type failingCache struct{ cache.Cache }

func (f failingCache) Put(context.Context, string, string, cache.PutOptions) error {
	return fmt.Errorf("write failed")
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
		{"storage", failingCache{cache.NewInMemoryCache()}, &fakeEmbedder{version: "test"}, "Broccoli", "write failed"},
		{"empty", cache.NewInMemoryCache(), &fakeEmbedder{version: "test"}, " ", "empty description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.c, tc.backend).EmbedIngredients(t.Context(), []ai.InputIngredient{{Description: tc.description}})
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, got)
		})
	}
}
