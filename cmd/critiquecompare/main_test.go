package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
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

func TestRunComparesNewestRecipesAndPrintsLargestDeltasFirst(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	now := time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC)
	seedRecipeWithCritique(t, cacheStore, "Old Hash", testRecipe("Old Stew"), cachedCritique(1, "old cached", "pro", now.Add(-3*time.Hour)))
	seedRecipeWithCritique(t, cacheStore, "Mid Hash", testRecipe("Mid Pasta"), cachedCritique(6, "mid cached", "pro", now.Add(-2*time.Hour)))
	seedRecipeWithCritique(t, cacheStore, "New Hash", testRecipe("New Tacos"), cachedCritique(9, "new cached", "pro", now.Add(-1*time.Hour)))
	require.NoError(t, critique.NewStore(cacheStore).Save(t.Context(), "Missing Hash", cachedCritique(4, "missing cached", "pro", now)))

	fake := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Mid Pasta": cachedCritique(9, "mid flash", "flash", now),
		"New Tacos": cachedCritique(7, "new flash", "flash", now),
	}}
	var out bytes.Buffer

	err := runWithDeps(t.Context(), []string{"-n", "2"}, &out, compareDeps{cache: cacheStore, critiquer: fake})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"New Tacos", "Mid Pasta"}, fake.Calls())
	body := out.String()
	assert.Contains(t, body, "Compared 2 recipes (1 cached critiques skipped because the recipe was missing)")
	assert.Contains(t, body, "DELTA")
	assert.Contains(t, body, "Mid Pasta")
	assert.Contains(t, body, "New Tacos")
	assert.NotContains(t, body, "Old Stew")
	assert.Less(t, strings.Index(body, "+3"), strings.Index(body, "-2"))
}

func TestRunReusesSavedBenchmarkCritiquesUnlessRefreshIsSet(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	now := time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC)
	recipe := testRecipe("Roast Chicken")
	hash := "Roast Hash"
	seedRecipeWithCritique(t, cacheStore, hash, recipe, cachedCritique(8, "cached", "pro", now))

	first := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Roast Chicken": cachedCritique(5, "first flash", "flash", now),
	}}
	var out bytes.Buffer
	require.NoError(t, runWithDeps(t.Context(), []string{"-n", "1", "-model", "gemini-test-flash"}, &out, compareDeps{cache: cacheStore, critiquer: first}))
	require.Equal(t, []string{"Roast Chicken"}, first.Calls())

	second := &fakeCritiquer{responses: map[string]*ai.RecipeCritique{
		"Roast Chicken": cachedCritique(10, "second flash", "flash", now),
	}}
	out.Reset()
	require.NoError(t, runWithDeps(t.Context(), []string{"-n", "1", "-model", "gemini-test-flash"}, &out, compareDeps{cache: cacheStore, critiquer: second}))
	assert.Empty(t, second.Calls())
	assert.Contains(t, out.String(), "first flash")
	assert.NotContains(t, out.String(), "second flash")

	out.Reset()
	require.NoError(t, runWithDeps(t.Context(), []string{"-n", "1", "-model", "gemini-test-flash", "-refresh"}, &out, compareDeps{cache: cacheStore, critiquer: second}))
	assert.Equal(t, []string{"Roast Chicken"}, second.Calls())
	assert.Contains(t, out.String(), "second flash")
}

func TestRunRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := runWithDeps(t.Context(), []string{"-n", "0"}, &out, compareDeps{cache: cache.NewInMemoryCache(), critiquer: &fakeCritiquer{}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "-n")
}

func TestRunRejectsInvalidParallelLimit(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := runWithDeps(t.Context(), []string{"-parallel", "0"}, &out, compareDeps{cache: cache.NewInMemoryCache(), critiquer: &fakeCritiquer{}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "-parallel")
}

func TestRunHonorsParallelLimit(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	now := time.Date(2026, time.July, 7, 12, 0, 0, 0, time.UTC)
	seedRecipeWithCritique(t, cacheStore, "One Hash", testRecipe("One"), cachedCritique(8, "one cached", "pro", now))
	seedRecipeWithCritique(t, cacheStore, "Two Hash", testRecipe("Two"), cachedCritique(8, "two cached", "pro", now.Add(-time.Minute)))
	seedRecipeWithCritique(t, cacheStore, "Three Hash", testRecipe("Three"), cachedCritique(8, "three cached", "pro", now.Add(-2*time.Minute)))

	fake := &blockingCritiquer{
		responses: map[string]*ai.RecipeCritique{
			"One":   cachedCritique(7, "one flash", "flash", now),
			"Two":   cachedCritique(6, "two flash", "flash", now),
			"Three": cachedCritique(5, "three flash", "flash", now),
		},
		release: make(chan struct{}),
	}
	var out bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		errCh <- runWithDeps(t.Context(), []string{"-n", "3", "-parallel", "2"}, &out, compareDeps{cache: cacheStore, critiquer: fake})
	}()

	require.Eventually(t, func() bool {
		return fake.MaxActive() == 2
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 2, fake.MaxActive())

	close(fake.release)
	require.NoError(t, <-errCh)
	assert.Contains(t, out.String(), "Compared 3 recipes")
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

type blockingCritiquer struct {
	mu        sync.Mutex
	active    int
	maxActive int
	responses map[string]*ai.RecipeCritique
	release   chan struct{}
}

func (b *blockingCritiquer) CritiqueRecipe(_ context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	b.mu.Lock()
	b.active++
	b.maxActive = max(b.maxActive, b.active)
	b.mu.Unlock()

	<-b.release

	b.mu.Lock()
	b.active--
	b.mu.Unlock()

	return b.responses[recipe.Title], nil
}

func (b *blockingCritiquer) MaxActive() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxActive
}
