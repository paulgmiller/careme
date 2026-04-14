package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestBuildRecipeCritiquePrompt(t *testing.T) {
	recipe := Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		CookTime:     "45 minutes",
		CostEstimate: "$18-24",
		Ingredients: []Ingredient{
			{Name: "Chicken", Quantity: "1 whole", Price: "$12"},
			{Name: "Lemon", Quantity: "1", Price: "$1"},
		},
		Instructions: []string{"Roast until golden.", "Finish with lemon juice."},
		Health:       "Balanced dinner",
		DrinkPairing: "Pinot Noir",
		OriginHash:   "internal-metadata",
		Saved:        true,
	}

	prompt, err := buildRecipeCritiquePrompt(recipe)
	require.NoError(t, err)
	for _, want := range []string{
		`"title": "Roast Chicken"`,
		`"cook_time": "45 minutes"`,
		`"name": "Chicken"`,
		`"quantity": "1 whole"`,
		`"price": "$12"`,
		`"instructions": [`,
		`"Roast until golden."`,
		`Recipe JSON:`,
		`Return JSON only using schema_version "recipe-critique-v1".`,
	} {
		assert.Contains(t, prompt, want)
	}
	for _, unwanted := range []string{
		`"origin_hash"`,
		`"previously_saved"`,
	} {
		assert.NotContains(t, prompt, unwanted)
	}
}

func TestParseRecipeCritique(t *testing.T) {
	critique, err := parseRecipeCritique(`{
		"schema_version": "recipe-critique-v1",
		"overall_score": 8,
		"summary": "Strong draft.",
		"strengths": ["balanced flavors"],
		"issues": [{"severity": "HIGH", "category": "Timing", "detail": "Reduce the sauce longer."}],
		"suggested_fixes": [" simmer longer "]
	}`)
	require.NoError(t, err)
	assert.Equal(t, "Strong draft.", critique.Summary)
	require.Len(t, critique.Strengths, 1)
	assert.Equal(t, "balanced flavors", critique.Strengths[0])
	require.Len(t, critique.Issues, 1)
	assert.Equal(t, "HIGH", critique.Issues[0].Severity)
	assert.Equal(t, "Timing", critique.Issues[0].Category)
	assert.Equal(t, "Reduce the sauce longer.", critique.Issues[0].Detail)
	require.Len(t, critique.SuggestedFixes, 1)
	assert.Equal(t, " simmer longer ", critique.SuggestedFixes[0])
}

func TestParseRecipeCritiqueRequiresScoreRange(t *testing.T) {
	_, err := parseRecipeCritique(`{"schema_version":"recipe-critique-v1","overall_score":11,"summary":"too high","strengths":[],"issues":[],"suggested_fixes":[]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overall score")
}

func TestRecipeCritiqueJSONSchemaTracksStruct(t *testing.T) {
	schema := recipeCritiqueJSONSchema()

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "expected top-level properties object, got %#v", schema["properties"])
	assert.Contains(t, properties, "schema_version")
	assert.Contains(t, properties, "overall_score")
	assert.NotContains(t, properties, "model")
	assert.NotContains(t, properties, "critiqued_at")

	overallScore, ok := properties["overall_score"].(map[string]any)
	require.True(t, ok, "expected overall_score schema object, got %#v", properties["overall_score"])
	assert.Equal(t, float64(1), overallScore["minimum"])
	assert.Equal(t, float64(10), overallScore["maximum"])
}

func TestGeminiUsageLogValue(t *testing.T) {
	t.Run("nil usage", func(t *testing.T) {
		raw, ok := geminiUsageLogValue(nil).(json.RawMessage)
		require.True(t, ok)
		assert.JSONEq(t, `null`, string(raw))
	})

	t.Run("usage marshals to json", func(t *testing.T) {
		raw, ok := geminiUsageLogValue(&genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     448,
			CandidatesTokenCount: 986,
			TotalTokenCount:      1877,
		}).(json.RawMessage)
		require.True(t, ok)
		assert.JSONEq(t, `{
			"promptTokenCount": 448,
			"candidatesTokenCount": 986,
			"totalTokenCount": 1877
		}`, string(raw))
	})
}
