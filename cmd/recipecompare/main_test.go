package main

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"
	"careme/internal/recipes/prompts"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReplayer struct {
	mu        sync.Mutex
	responses map[string]*ai.Recipe
	calls     []*ai.PromptRecord
}

func (f *fakeReplayer) ReplayRecipePrompt(_ context.Context, record *ai.PromptRecord) (*ai.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *record
	clone.Input = append([]ai.PromptMessage(nil), record.Input...)
	f.calls = append(f.calls, &clone)
	return f.responses[record.ResponseID], nil
}

func (f *fakeReplayer) Calls() []*ai.PromptRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

type fakeJudge struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeJudge) CompareRecipes(_ context.Context, original, candidate ai.Recipe) (*ai.RecipeComparison, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, original.Title+" -> "+candidate.Title)
	return &ai.RecipeComparison{
		SchemaVersion:  "recipe-comparison-v1",
		Winner:         ai.RecipeComparisonWinnerCandidate,
		OriginalScore:  7,
		CandidateScore: 9,
		Summary:        "candidate is clearer",
		Reasons:        []string{"better prep order"},
		Model:          "gemini-test",
	}, nil
}

func (f *fakeJudge) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func TestCompareInputsAcceptsShoppingAndRecipeHashesAndDeduplicates(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	original := compareTestRecipe("Old Dinner", "child-response")
	candidate := compareTestRecipe("New Dinner", "new-response")
	originalHash := original.ComputeHash()
	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), original))
	require.NoError(t, recipes.IO(cacheStore).SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{original}}, "shopping-a"))
	seedPromptRecord(t, cacheStore, ai.PromptRecord{
		ResponseID:   "parent-response",
		Instructions: "menu instructions",
		Input:        []ai.PromptMessage{{Role: "user", Content: "ingredient TSV"}},
	})
	seedPromptRecord(t, cacheStore, ai.PromptRecord{
		ResponseID:         "child-response",
		Instructions:       "recipe instructions",
		PreviousResponseID: "parent-response",
		Input:              []ai.PromptMessage{{Role: "user", Content: "make tacos"}},
	})
	replayer := &fakeReplayer{responses: map[string]*ai.Recipe{"child-response": &candidate}}
	judge := &fakeJudge{}

	rows, err := compareInputs(t.Context(), cacheStore, replayer, judge, "gpt-5.6-sol", []string{"shopping-a"}, []string{originalHash}, false)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Skipped)
	require.NoError(t, rows[0].Err)
	require.NotNil(t, rows[0].Artifact)
	assert.Equal(t, originalHash, rows[0].Hash)
	assert.Equal(t, "shoppinglist", rows[0].SourceKind)
	assert.Equal(t, "shopping-a", rows[0].SourceHash)
	assert.Equal(t, ai.RecipeComparisonWinnerCandidate, rows[0].Artifact.Comparison.Winner)
	assert.Equal(t, []string{"Old Dinner -> New Dinner"}, judge.Calls())

	calls := replayer.Calls()
	require.Len(t, calls, 1)
	require.Len(t, calls[0].Input, 2)
	assert.Equal(t, "ingredient TSV", calls[0].Input[0].Content)
	assert.Equal(t, "make tacos", calls[0].Input[1].Content)
	assert.Equal(t, "child-response", calls[0].ResponseID)
}

func TestCompareInputsReusesCachedComparisonUnlessRefresh(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	original := compareTestRecipe("Old Dinner", "child-response")
	originalHash := original.ComputeHash()
	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), original))
	seedPromptRecord(t, cacheStore, ai.PromptRecord{
		ResponseID:   "child-response",
		Instructions: "recipe instructions",
		Input:        []ai.PromptMessage{{Role: "user", Content: "make tacos"}},
	})
	store := comparisonStore{cache: cacheStore, model: "gpt-5.6-sol"}
	require.NoError(t, store.Save(t.Context(), originalHash, &comparisonArtifact{
		OriginalHash:    originalHash,
		OriginalTitle:   original.Title,
		TargetModel:     "gpt-5.6-sol",
		CandidateRecipe: compareTestRecipe("Cached New Dinner", "cached-response"),
		Comparison: &ai.RecipeComparison{
			SchemaVersion:  "recipe-comparison-v1",
			Winner:         ai.RecipeComparisonWinnerOriginal,
			OriginalScore:  8,
			CandidateScore: 7,
			Summary:        "cached",
		},
	}))
	replayer := &fakeReplayer{responses: map[string]*ai.Recipe{
		"child-response": ptr(compareTestRecipe("Fresh New Dinner", "fresh-response")),
	}}
	judge := &fakeJudge{}

	rows, err := compareInputs(t.Context(), cacheStore, replayer, judge, "gpt-5.6-sol", nil, []string{originalHash}, false)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, replayer.Calls())
	assert.Empty(t, judge.Calls())
	assert.Equal(t, "cached", rows[0].Artifact.Comparison.Summary)

	rows, err = compareInputs(t.Context(), cacheStore, replayer, judge, "gpt-5.6-sol", nil, []string{originalHash}, true)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Len(t, replayer.Calls(), 1)
	assert.Equal(t, []string{"Old Dinner -> Fresh New Dinner"}, judge.Calls())
	assert.Equal(t, "candidate is clearer", rows[0].Artifact.Comparison.Summary)
}

func TestCompareInputsSkipsRecipeMissingPromptRecord(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	original := compareTestRecipe("Old Dinner", "missing-response")
	originalHash := original.ComputeHash()
	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), original))

	rows, err := compareInputs(t.Context(), cacheStore, &fakeReplayer{}, &fakeJudge{}, "gpt-5.6-sol", nil, []string{originalHash}, false)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "missing recipe prompt record", rows[0].Skipped)
}

func TestPrintRowsIncludesSummaryStatsAndRows(t *testing.T) {
	t.Parallel()

	rows := []comparisonRow{
		{
			SourceKind: "recipe",
			SourceHash: "hash-a",
			Hash:       "hash-a",
			Original:   compareTestRecipe("Old Dinner", "old-response"),
			Artifact: &comparisonArtifact{
				CandidateRecipe: compareTestRecipe("New Dinner", "new-response"),
				Comparison: &ai.RecipeComparison{
					Winner:  ai.RecipeComparisonWinnerCandidate,
					Summary: "candidate is clearer",
				},
			},
		},
		{SourceKind: "recipe", SourceHash: "hash-b", Hash: "hash-b", Skipped: "missing recipe response ID"},
		{SourceKind: "recipe", SourceHash: "hash-c", Hash: "hash-c", Err: assert.AnError},
	}
	var out bytes.Buffer

	require.NoError(t, printRows(&out, rows))

	body := out.String()
	assert.Contains(t, body, "Compared 1 recipes; skipped=1 errors=1 original_wins=0 candidate_wins=1 ties=0")
	assert.Contains(t, body, "OK\trecipe\thash-a\thash-a\tOld Dinner\tNew Dinner\tcandidate\tcandidate is clearer")
	assert.Contains(t, body, "SKIP\trecipe\thash-b\thash-b\tmissing recipe response ID")
	assert.Contains(t, body, "ERROR\trecipe\thash-c\thash-c")
}

func compareTestRecipe(title, responseID string) ai.Recipe {
	return ai.Recipe{
		Title:        title,
		Description:  "Dinner.",
		CookTime:     "30 minutes",
		CostEstimate: "$12-15",
		Ingredients:  []ai.Ingredient{{Name: "Chicken", Quantity: "1 pound"}},
		Instructions: []string{"Chop the chicken.", "Cook until done.", "Serve warm."},
		Health:       "Balanced.",
		DrinkPairing: "Water.",
		ResponseID:   responseID,
	}
}

func seedPromptRecord(t *testing.T, cacheStore cache.ListCache, record ai.PromptRecord) {
	t.Helper()
	body, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, cacheStore.Put(t.Context(), prompts.CachePrefix+record.ResponseID, string(body), cache.Unconditional()))
}

func ptr[T any](value T) *T {
	return &value
}
