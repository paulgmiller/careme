package main

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/feedback"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCritiquer struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCritiquer) CritiqueRecipe(_ context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recipe.Title)
	return &ai.RecipeCritique{OverallScore: len(recipe.Title), Model: "response-model", Summary: "evaluated"}, nil
}

func (f *fakeCritiquer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func TestBuildDatasetSelectsLatestCookedRecipesForUser(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)

	alpha := saveRecipe(t, c, "Alpha")
	beta := saveRecipe(t, c, "Beta")
	gamma := saveRecipe(t, c, "Gamma")
	other := saveRecipe(t, c, "Other user")
	seedUser(t, c, "paul", "paul.miller@gmail.com", []utypes.Recipe{
		{Title: alpha.Title, Hash: alpha.ComputeHash()},
		{Title: beta.Title, Hash: beta.ComputeHash()},
		{Title: gamma.Title, Hash: gamma.ComputeHash()},
	})
	seedUser(t, c, "other", "other@example.com", []utypes.Recipe{{Title: other.Title, Hash: other.ComputeHash()}})

	feedbackIO := feedback.NewIO(c)
	require.NoError(t, feedbackIO.SaveFeedback(t.Context(), alpha.ComputeHash(), feedback.Feedback{Cooked: true, Stars: 5, Comment: "private", UpdatedAt: now.Add(-time.Hour)}))
	require.NoError(t, feedbackIO.SaveFeedback(t.Context(), beta.ComputeHash(), feedback.Feedback{Cooked: true, UpdatedAt: now}))
	require.NoError(t, feedbackIO.SaveFeedback(t.Context(), gamma.ComputeHash(), feedback.Feedback{Cooked: false, Stars: 2, UpdatedAt: now.Add(time.Hour)}))
	require.NoError(t, feedbackIO.SaveFeedback(t.Context(), other.ComputeHash(), feedback.Feedback{Cooked: true, Stars: 1, UpdatedAt: now.Add(2 * time.Hour)}))
	require.NoError(t, critique.NewStore(c).Save(t.Context(), beta.ComputeHash(), &ai.RecipeCritique{OverallScore: 7, Model: "historical"}))

	dataset, err := buildDataset(t.Context(), c, "paul.miller@gmail.com", "cooked-2026-08-06", 2, now)

	require.NoError(t, err)
	require.Len(t, dataset.Samples, 2)
	assert.Equal(t, "Beta", dataset.Samples[0].Title)
	assert.Zero(t, dataset.Samples[0].Stars)
	require.NotNil(t, dataset.Samples[0].HistoricalCritique)
	assert.Equal(t, "Alpha", dataset.Samples[1].Title)
	assert.Equal(t, 5, dataset.Samples[1].Stars)
	assert.NotEmpty(t, dataset.ID)
	body, err := json.Marshal(dataset)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "paul.miller@gmail.com")
	assert.NotContains(t, string(body), "private")
}

func TestEvalDatasetNamesAreImmutable(t *testing.T) {
	t.Parallel()
	store := evalStore{cache: cache.NewInMemoryCache()}
	dataset := &evalDataset{Name: "snapshot", ID: "id", SchemaVersion: "v1"}

	require.NoError(t, store.saveDataset(t.Context(), dataset))
	err := store.saveDataset(t.Context(), dataset)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestEvaluateModelCachesByModelAndPrompt(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	store := evalStore{cache: c}
	dataset := &evalDataset{ID: "dataset", Samples: []evalSample{
		{Hash: "a", Title: "Alpha", Recipe: ai.Recipe{Title: "Alpha"}},
		{Hash: "b", Title: "Beta", Recipe: ai.Recipe{Title: "Beta"}},
	}}
	first := &fakeCritiquer{}
	require.NoError(t, evaluateModel(t.Context(), store, dataset, "model-a", first, false, 2))
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, first.Calls())

	second := &fakeCritiquer{}
	require.NoError(t, evaluateModel(t.Context(), store, dataset, "model-a", second, false, 2))
	assert.Empty(t, second.Calls())

	require.NoError(t, evaluateModel(t.Context(), store, dataset, "model-b", second, false, 2))
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, second.Calls())

	third := &fakeCritiquer{}
	require.NoError(t, evaluateModel(t.Context(), store, dataset, "model-a", third, true, 2))
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, third.Calls())
}

func TestCalculateModelStatsUsesOnlyRatedRowsForAlignment(t *testing.T) {
	t.Parallel()
	samples := []evalSample{
		{Hash: "a", Stars: 2},
		{Hash: "b", Stars: 4},
		{Hash: "c"},
	}
	results := map[string]evalResult{
		"a": {Critique: &ai.RecipeCritique{OverallScore: 4}},
		"b": {Critique: &ai.RecipeCritique{OverallScore: 8}},
		"c": {Critique: &ai.RecipeCritique{OverallScore: 10}},
	}

	stats := calculateModelStats(samples, results)

	assert.Equal(t, 3, stats.count)
	assert.Zero(t, stats.missing)
	assert.Equal(t, 2, stats.ratedCount)
	assert.InDelta(t, 0, stats.mae, 0.0001)
	assert.InDelta(t, 1, stats.pearson, 0.0001)
	assert.InDelta(t, 1, stats.spearman, 0.0001)
	assert.InDelta(t, 2.0/3.0, stats.passRate, 0.0001)
}

func TestPrintReportIncludesRowsStatsAndPairwiseComparison(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	store := evalStore{cache: c}
	dataset := &evalDataset{Name: "snapshot", ID: "dataset", Samples: []evalSample{{Hash: "a", Title: "Alpha", Stars: 5}}}
	for model, score := range map[string]int{"model-a": 7, "model-b": 9} {
		require.NoError(t, store.saveResult(t.Context(), evalResult{DatasetID: dataset.ID, PromptFingerprint: "prompt", RequestedModel: model, RecipeHash: "a", Critique: &ai.RecipeCritique{OverallScore: score}}))
	}
	var out bytes.Buffer

	require.NoError(t, printReport(t.Context(), &out, store, dataset, nil, ""))

	body := out.String()
	assert.Contains(t, body, "Alpha\ta")
	assert.Contains(t, body, "model-a")
	assert.Contains(t, body, "7/fail")
	assert.Contains(t, body, "9/pass")
	assert.Contains(t, body, "PAIR")
	assert.Contains(t, body, "model-a -> model-b\t1\t+2.00\t1\t2")
}

func saveRecipe(t *testing.T, c cache.ListCache, title string) ai.Recipe {
	t.Helper()
	recipe := ai.Recipe{Title: title, Description: "Dinner", Instructions: []string{"Cook it."}}
	require.NoError(t, recipes.IO(c).SaveRecipe(t.Context(), recipe))
	return recipe
}

func seedUser(t *testing.T, c cache.ListCache, id, email string, lastRecipes []utypes.Recipe) {
	t.Helper()
	body, err := json.Marshal(utypes.User{ID: id, Email: []string{email}, ShoppingDay: time.Saturday.String(), LastRecipes: lastRecipes})
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), "users/"+id, string(body), cache.Unconditional()))
	require.NoError(t, c.Put(t.Context(), "email2user/"+email, id, cache.Unconditional()))
}
