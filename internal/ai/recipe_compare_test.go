package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRecipeComparisonPrompt(t *testing.T) {
	original := Recipe{
		Title:        "Old Chicken",
		Description:  "Original dinner.",
		ResponseID:   "old-response",
		OriginHash:   "origin",
		ParentHash:   "parent",
		Instructions: []string{"Cook it."},
	}
	candidate := Recipe{
		Title:        "New Chicken",
		Description:  "Candidate dinner.",
		ResponseID:   "new-response",
		OriginHash:   "candidate-origin",
		ParentHash:   "candidate-parent",
		Instructions: []string{"Prep it.", "Cook it."},
	}

	prompt, err := buildRecipeComparisonPrompt(original, candidate)

	require.NoError(t, err)
	assert.Contains(t, prompt, `schema_version "recipe-comparison-v1"`)
	assert.Contains(t, prompt, "Original recipe JSON:")
	assert.Contains(t, prompt, `"title": "Old Chicken"`)
	assert.Contains(t, prompt, "Candidate recipe JSON:")
	assert.Contains(t, prompt, `"title": "New Chicken"`)
	assert.NotContains(t, prompt, `"origin_hash"`)
	assert.NotContains(t, prompt, `"parent_hash"`)
}

func TestParseRecipeComparison(t *testing.T) {
	comparison, err := parseRecipeComparison(`{
		"schema_version": "recipe-comparison-v1",
		"winner": "candidate",
		"original_score": 7,
		"candidate_score": 9,
		"summary": "Candidate is clearer.",
		"reasons": ["better prep order"]
	}`)

	require.NoError(t, err)
	assert.Equal(t, recipeComparisonSchemaV1, comparison.SchemaVersion)
	assert.Equal(t, RecipeComparisonWinnerCandidate, comparison.Winner)
	assert.Equal(t, 7, comparison.OriginalScore)
	assert.Equal(t, 9, comparison.CandidateScore)
	assert.Equal(t, "Candidate is clearer.", comparison.Summary)
	assert.Equal(t, []string{"better prep order"}, comparison.Reasons)
}

func TestParseRecipeComparisonValidatesWinnerAndScores(t *testing.T) {
	_, err := parseRecipeComparison(`{
		"schema_version": "recipe-comparison-v1",
		"winner": "new",
		"original_score": 7,
		"candidate_score": 9,
		"summary": "Candidate is clearer.",
		"reasons": []
	}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "winner")

	_, err = parseRecipeComparison(`{
		"schema_version": "recipe-comparison-v1",
		"winner": "candidate",
		"original_score": 0,
		"candidate_score": 9,
		"summary": "Candidate is clearer.",
		"reasons": []
	}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "original score")
}

func TestRecipeComparisonJSONSchemaTracksStruct(t *testing.T) {
	schema := recipeComparisonJSONSchema()

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "expected top-level properties object, got %#v", schema["properties"])
	assert.Contains(t, properties, "schema_version")
	assert.Contains(t, properties, "winner")
	assert.Contains(t, properties, "original_score")
	assert.Contains(t, properties, "candidate_score")
	assert.NotContains(t, properties, "model")
	assert.NotContains(t, properties, "compared_at")
}
