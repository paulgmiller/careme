package grading

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSaveLoadUsesPrefixedKey(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(cache.NewFileCache(tmpDir))
	key := cacheKey("location-hash", "ingredient-hash")
	grade := &ai.IngredientGrade{
		SchemaVersion: "ingredient-grade-v1",
		Score:         8,
		Decision:      ai.IngredientDecisionKeep,
		Reason:        "Fresh produce with broad recipe use.",
		Ingredient:    ai.IngredientSnapshot{Description: "Asparagus"},
		GradedAt:      time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
	}

	require.NoError(t, store.Save(t.Context(), key, grade))
	_, err := os.Stat(filepath.Join(tmpDir, cachePrefix, "location-hash", "ingredient-hash"))
	require.NoError(t, err)

	got, err := store.Load(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, grade.Score, got.Score)
	assert.Equal(t, grade.Reason, got.Reason)
	assert.Equal(t, grade.Ingredient.Description, got.Ingredient.Description)
}
