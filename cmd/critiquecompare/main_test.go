package main

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCritiquer struct {
	mu        sync.Mutex
	responses map[string]*ai.RecipeCritique
	calls     []string
}

func (f *fakeCritiquer) CritiqueRecipe(_ context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recipe.Title)
	return f.responses[recipe.Title], nil
}

func (f *fakeCritiquer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func TestCompareCritiquesComparesLimitedCachedRecipes(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	now := time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC)
	seedRecipeWithCritique(t, cacheStore, "a", testRecipe("Alpha"), cachedCritique(8, "alpha cached", "pro", now))
	seedRecipeWithCritique(t, cacheStore, "b", testRecipe("Beta"), cachedCritique(6, "beta cached", "pro", now))
	seedRecipeWithCritique(t, cacheStore, "c", testRecipe("Gamma"), cachedCritique(4, "gamma cached", "pro", now))

	fake := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Alpha": cachedCritique(7, "alpha flash", "flash", now),
		"Beta":  cachedCritique(9, "beta flash", "flash", now),
	}}

	rows, err := compareCritiques(t.Context(), cacheStore, fake, "gemini-test-flash", 2, false)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, fake.Calls())
	require.Len(t, rows, 2)
	assert.Equal(t, "a", rows[0].Hash)
	assert.Equal(t, -1, rows[0].ScoreDelta)
	assert.Equal(t, 1, rows[0].AbsScoreDelta)
	assert.Equal(t, "b", rows[1].Hash)
	assert.Equal(t, 3, rows[1].ScoreDelta)
	assert.Equal(t, 3, rows[1].AbsScoreDelta)
}

func TestCompareCritiquesReusesSavedBenchmarkCritiquesUnlessRefreshIsSet(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	now := time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC)
	seedRecipeWithCritique(t, cacheStore, "roast", testRecipe("Roast Chicken"), cachedCritique(8, "cached", "pro", now))

	first := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Roast Chicken": cachedCritique(5, "first flash", "flash", now),
	}}
	rows, err := compareCritiques(t.Context(), cacheStore, first, "gemini-test-flash", 1, false)
	require.NoError(t, err)
	require.Equal(t, []string{"Roast Chicken"}, first.Calls())
	require.Len(t, rows, 1)
	assert.Equal(t, "first flash", rows[0].Flash.Summary)

	second := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Roast Chicken": cachedCritique(10, "second flash", "flash", now),
	}}
	rows, err = compareCritiques(t.Context(), cacheStore, second, "gemini-test-flash", 1, false)
	require.NoError(t, err)
	assert.Empty(t, second.Calls())
	require.Len(t, rows, 1)
	assert.Equal(t, "first flash", rows[0].Flash.Summary)

	rows, err = compareCritiques(t.Context(), cacheStore, second, "gemini-test-flash", 1, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"Roast Chicken"}, second.Calls())
	require.Len(t, rows, 1)
	assert.Equal(t, "second flash", rows[0].Flash.Summary)
}

func TestPrintRowsIncludesStats(t *testing.T) {
	t.Parallel()

	rows := []comparisonRow{
		{
			Hash:          "a",
			Title:         "Alpha",
			Cached:        fullCritique(6, "alpha cached", "pro"),
			Flash:         fullCritique(8, "alpha flash", "flash"),
			ScoreDelta:    2,
			AbsScoreDelta: 2,
		},
		{
			Hash:          "b",
			Title:         "Beta",
			Cached:        fullCritique(9, "beta cached", "pro"),
			Flash:         fullCritique(8, "beta flash", "flash"),
			ScoreDelta:    -1,
			AbsScoreDelta: 1,
		},
	}
	var out bytes.Buffer

	require.NoError(t, printRows(&out, rows))

	body := out.String()
	assert.Contains(t, body, "Compared 2 recipes")
	assert.Contains(t, body, "Showing 1 critiques with score difference > 1")
	assert.Contains(t, body, "Alpha")
	assert.NotContains(t, body, "Beta (b)")
	assert.Contains(t, body, "Cached critique")
	assert.Contains(t, body, "Flash critique")
	assert.Contains(t, body, "Score delta: +2 (cached 6, flash 8)")
	assert.Contains(t, body, "Summary: alpha cached")
	assert.Contains(t, body, "Strengths:\n- clear prep")
	assert.Contains(t, body, "Issues:\n- [timing/high] timing is off")
	assert.Contains(t, body, "Suggested fixes:\n- fix timing")
	assert.Contains(t, body, "STATS\tCACHED\tFLASH\tDELTA")
	assert.Contains(t, body, "AVERAGE\t7.50\t8.00\t+0.50")
	assert.Contains(t, body, "VARIANCE\t2.25\t0.00\t2.25")
	assert.Contains(t, body, "TOTAL_DIFFERENCE\t1\tABS 3")
}

func seedRecipeWithCritique(t *testing.T, cacheStore cache.ListCache, hash string, recipe ai.Recipe, c *ai.RecipeCritique) {
	t.Helper()

	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), recipe))
	actualHash := recipe.ComputeHash()
	if actualHash != hash {
		reader, err := cacheStore.Get(t.Context(), "recipe/"+actualHash)
		require.NoError(t, err)
		defer func() {
			_ = reader.Close()
		}()
		var buf bytes.Buffer
		_, err = buf.ReadFrom(reader)
		require.NoError(t, err)
		require.NoError(t, cacheStore.Put(t.Context(), "recipe/"+hash, buf.String(), cache.Unconditional()))
	}
	require.NoError(t, critique.NewStore(cacheStore).Save(t.Context(), hash, c))
}

func testRecipe(title string) ai.Recipe {
	return ai.Recipe{
		Title:        title,
		Description:  "A practical dinner.",
		CookTime:     "35 minutes",
		CostEstimate: "$12-16",
		Ingredients: []ai.Ingredient{
			{Name: "Chicken", Quantity: "1 pound", Price: "$8"},
			{Name: "Greens", Quantity: "1 bunch", Price: "$4"},
		},
		Instructions: []string{"Chop the greens.", "Cook everything until done.", "Serve warm."},
		Health:       "Balanced.",
		DrinkPairing: "Water works well.",
	}
}

func cachedCritique(score int, summary string, model string, critiquedAt time.Time) *ai.RecipeCritique {
	return &ai.RecipeCritique{
		SchemaVersion: "recipe-critique-v1",
		OverallScore:  score,
		Summary:       summary,
		Model:         model,
		CritiquedAt:   critiquedAt,
	}
}

func fullCritique(score int, summary string, model string) *ai.RecipeCritique {
	c := cachedCritique(score, summary, model, time.Time{})
	c.Strengths = []string{"clear prep"}
	c.Issues = []ai.RecipeCritiqueIssue{{Severity: "high", Category: "timing", Detail: "timing is off"}}
	c.SuggestedFixes = []string{"fix timing"}
	return c
}
