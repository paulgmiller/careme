package recipes

import (
	"bytes"
	"io"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeImageStoreKeepsSketchAndLegacyPhotoSeparate(t *testing.T) {
	store := NewImageStore(cache.NewFileCache(t.TempDir()))
	const hash = "same-recipe"
	require.NoError(t, store.Save(t.Context(), hash, ai.RecipeImagePhoto, &ai.GeneratedImage{Body: bytes.NewBufferString("old photo")}))

	exists, err := store.Exists(t.Context(), hash, ai.RecipeImageSketch)
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, store.Save(t.Context(), hash, ai.RecipeImageSketch, &ai.GeneratedImage{Body: bytes.NewBufferString("new sketch")}))
	for _, test := range []struct {
		style ai.RecipeImageStyle
		want  string
	}{
		{ai.RecipeImagePhoto, "old photo"},
		{ai.RecipeImageSketch, "new sketch"},
	} {
		body, err := store.FromCache(t.Context(), hash, test.style)
		require.NoError(t, err)
		got, err := io.ReadAll(body)
		require.NoError(t, err)
		require.NoError(t, body.Close())
		assert.Equal(t, test.want, string(got))
	}
}
