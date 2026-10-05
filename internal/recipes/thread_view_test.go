package recipes

import (
	"slices"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThreadViewsPreserveInputOrder(t *testing.T) {
	t.Parallel()
	older := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	thread := []RecipeThreadEntry{
		{Question: "older", ResponseID: "old-response", CreatedAt: older},
		{Question: "newest without response", CreatedAt: older.Add(2 * time.Hour)},
		{Question: "newer", ResponseID: " new-response ", CreatedAt: older.Add(time.Hour)},
	}
	original := slices.Clone(thread)
	assert.Equal(t, "new-response", latestThreadResponseID(thread))
	assert.Equal(t, original, thread)

	fragment := newRecipeThreadView(thread, true, ai.ResponseRef{ID: "new-response"}, "recipe-hash")
	params := DefaultParams(&locations.Location{ID: "store"}, older)
	page, err := newRecipePageView(t.Context(), recipeViewInput{
		params: params,
		recipe: ai.Recipe{ResponseID: "initial"},
		thread: thread,
	})
	require.NoError(t, err)
	assert.Equal(t, "new-response", page.ResponseID)
	assert.Equal(t, fragment.Thread, page.Thread)
	assert.Equal(t, "newest without response", page.Thread[0].Question)
	assert.Equal(t, original, thread)
}

func TestLatestThreadResponseID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		thread []RecipeThreadEntry
		want   string
	}{
		{name: "empty"},
		{name: "blank", thread: []RecipeThreadEntry{{ResponseID: "  "}}},
		{name: "equal timestamps retain first", thread: []RecipeThreadEntry{{ResponseID: "first"}, {ResponseID: "second"}}, want: "first"},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, latestThreadResponseID(tc.thread)) })
	}
}
