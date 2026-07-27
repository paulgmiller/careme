package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRecipeCritiqueSystemInstructionChecksPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"do the instructions begin with preparation before active cooking starts",
		"does every mention of an ingredient in the instructions include the exact amount used in that step",
		"do the amounts used across instruction steps agree with each ingredient's total quantity in the ingredient list",
		"does the stated cook_time match the total time implied by all instruction steps, including prep, resting, and passive cooking",
	} {
		assert.Contains(t, recipeCritiqueSystemInstruction, want)
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

func TestCritiqueRecipeUsesOpenRouterStructuredOutput(t *testing.T) {
	client := NewCritiquer("openrouter-key", "anthropic/claude-sonnet-4.5", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "https", req.URL.Scheme)
			assert.Equal(t, "openrouter.ai", req.URL.Host)
			assert.Equal(t, "/api/v1/chat/completions", req.URL.Path)
			assert.Equal(t, "Bearer openrouter-key", req.Header.Get("Authorization"))
			assert.Equal(t, openRouterApplicationURL, req.Header.Get("HTTP-Referer"))
			assert.Equal(t, openRouterApplicationTitle, req.Header.Get("X-OpenRouter-Title"))

			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(body, &payload))
			assert.Equal(t, "anthropic/claude-sonnet-4.5", payload["model"])
			provider := payload["provider"].(map[string]any)
			assert.Equal(t, true, provider["require_parameters"])
			responseFormat := payload["response_format"].(map[string]any)
			assert.Equal(t, "json_schema", responseFormat["type"])
			jsonSchema := responseFormat["json_schema"].(map[string]any)
			assert.Equal(t, true, jsonSchema["strict"])

			content := `{"schema_version":"recipe-critique-v1","overall_score":8,"summary":"Ready to cook.","strengths":[],"issues":[],"suggested_fixes":[]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
					"id": "generation-123",
					"object": "chat.completion",
					"created": 1778529600,
					"model": "anthropic/claude-sonnet-4.5",
					"choices": [{
						"index": 0,
						"finish_reason": "stop",
						"message": {"role": "assistant", "content": %q}
					}],
					"usage": {
						"prompt_tokens": 100,
						"completion_tokens": 25,
						"total_tokens": 125,
						"cost": 0.00125
					}
				}`, content))),
				Request: req,
			}, nil
		}),
	})

	got, err := client.CritiqueRecipe(t.Context(), Recipe{Title: "Roast Chicken"})

	require.NoError(t, err)
	assert.Equal(t, 8, got.OverallScore)
	assert.Equal(t, "Ready to cook.", got.Summary)
	assert.Equal(t, "anthropic/claude-sonnet-4.5", got.Model)
	assert.False(t, got.CritiquedAt.IsZero())
}

func TestOpenRouterUsageLogAttr(t *testing.T) {
	t.Run("nil usage", func(t *testing.T) {
		attr := openRouterUsageLogAttr(nil)
		assert.Equal(t, "usage", attr.Key)
		assert.Equal(t, slog.KindGroup, attr.Value.Kind())
		require.Len(t, attr.Value.Group(), 1)
		assert.Equal(t, slog.Bool("available", false), attr.Value.Group()[0])
	})

	t.Run("usage becomes a slog group", func(t *testing.T) {
		var response openai.ChatCompletion
		require.NoError(t, json.Unmarshal([]byte(`{
			"id": "generation-123",
			"object": "chat.completion",
			"created": 1778529600,
			"model": "anthropic/claude-sonnet-4.5",
			"choices": [],
			"usage": {
				"prompt_tokens": 448,
				"prompt_tokens_details": {"cached_tokens": 22},
				"completion_tokens": 1097,
				"completion_tokens_details": {"reasoning_tokens": 111},
				"total_tokens": 1545,
				"cost": 0.0146404
			}
		}`), &response))

		attr := openRouterUsageLogAttr(&response)
		assert.Equal(t, "usage", attr.Key)
		assert.Equal(t, slog.KindGroup, attr.Value.Kind())
		assert.Equal(t, []slog.Attr{
			slog.Bool("available", true),
			slog.Int64("promptTokenCount", 448),
			slog.Int64("cachedTokenCount", 22),
			slog.Int64("completionTokenCount", 1097),
			slog.Int64("reasoningTokenCount", 111),
			slog.Int64("totalTokenCount", 1545),
			slog.Float64("costCredits", 0.0146404),
		}, attr.Value.Group())
	})
}
