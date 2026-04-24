package grading

import (
	"os"
	"path/filepath"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSaveLoadUsesPrefixedKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(cache.NewFileCache(tmpDir))
	key := cacheKey("ingredient-hash")
	ingredient := &ai.InputIngredient{
		ProductID:   "ingredient-123",
		Description: "Asparagus",
		Grade: &ai.IngredientGrade{
			Score:  8,
			Reason: "Fresh produce with broad recipe use.",
		},
	}

	require.NoError(t, store.Save(t.Context(), key, ingredient))
	_, err := os.Stat(filepath.Join(tmpDir, cachePrefix, "ingredient-hash"))
	require.NoError(t, err)

	got, err := store.Load(t.Context(), key)
	require.NoError(t, err)
	require.NotNil(t, got.Grade)
	assert.Equal(t, ingredient.Grade.Score, got.Grade.Score)
	assert.Equal(t, ingredient.Grade.Reason, got.Grade.Reason)
	assert.Equal(t, ingredient.Description, got.Description)
}
