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
		Title:       "Roast Chicken",
		Description: "Crisp skin and herbs.",
		Properties: RecipeProperties{
			TotalMinutes:         45,
			Servings:             4,
			EstimatedCostDollars: 21,
			CaloriesPerServing:   610,
			CookingMethods:       []CookingMethod{CookingMethodOven},
		},
		Ingredients: []Ingredient{
			{Name: "Chicken", Quantity: "1 whole", Price: "$12"},
			{Name: "Lemon", Quantity: "1", Price: "$1"},
		},
		Instructions: []string{
			"Prepare:\n\n- 1 whole chicken\n- 1 lemon, juiced",
			"Roast until golden.",
		},
		DrinkPairing: "Pinot Noir",
		OriginHash:   "internal-metadata",
	}

	prompt, err := buildRecipeCritiquePrompt(recipe)
	require.NoError(t, err)
	for _, want := range []string{
		`"title": "Roast Chicken"`,
		`"total_minutes": 45`,
		`"servings": 4`,
		`"estimated_cost_dollars": 21`,
		`"calories_per_serving": 610`,
		`"cooking_methods": [`,
		`"name": "Chicken"`,
		`"quantity": "1 whole"`,
		`"price": "$12"`,
		`"instructions": [`,
		`1 lemon, juiced`,
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

func TestRecipeCritiqueDefaultsAndFingerprint(t *testing.T) {
	t.Parallel()

	client := NewCritiquer("openrouter-key", "", http.DefaultClient)

	assert.Equal(t, "anthropic/claude-opus-5", client.model)
	assert.Len(t, RecipeCritiqueFingerprint(), 64)
	assert.Equal(t, RecipeCritiqueFingerprint(), RecipeCritiqueFingerprint())
}

func TestRecipeCritiqueSystemInstructionChecksPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"do the instructions begin with preparation before active cooking starts",
		"does each step cover one coherent task or component, keeping immediate actions on the same ingredient together",
		"when an ingredient is first used, does the instruction prose or a bullet include the exact amount used in that step",
		"are bullet lists limited to preparations or mixtures of more than three ingredients",
		"placed where those ingredients enter the action",
		"separated from surrounding prose by a blank line before and after the list",
		"do later steps refer concisely to named mixtures or prepared components without needlessly restating their constituent ingredients and amounts",
		"do the amounts used across instruction steps agree with each ingredient's total quantity in the ingredient list",
		"does properties.total_minutes match the total time implied by all instruction steps, including prep, resting, and passive cooking",
		"are the total time, serving yield, total cost, and calories-per-serving estimates plausible",
		"is health empty unless it explains a meaningful dietary or nutritional ingredient swap",
	} {
		assert.Contains(t, recipeCritiqueSystemInstruction, want)
	}
}

func TestRecipeCritiqueSystemInstructionRequiresProviderCompatibleOutput(t *testing.T) {
	for _, want := range []string{
		"overall_score must be an integer from 1 through 10",
		"summary must be a non-empty, concise sentence",
		"return one valid JSON object only",
	} {
		assert.Contains(t, recipeCritiqueSystemInstruction, want)
	}
}

func TestRecipeCritiqueSystemInstructionChecksSaltAtTheCorrectStage(t *testing.T) {
	for _, want := range []string{
		"1.25% salt by weight for boneless meat",
		"1.5% for bone-in meat including roast chicken",
		"1% for vegetables and grains",
		"2% salinity for pasta or vegetable-blanching water",
		"do not treat salt added later as a substitute for presalting meat or salting pasta or blanching water",
		"evaluate salt by weight when available rather than assuming equal volume measures across salt types",
		"reflect substantial underseasoning or oversalting in the overall score",
	} {
		assert.Contains(t, recipeCritiqueSystemInstruction, want)
	}
	assert.NotContains(t, recipeCritiqueSystemInstruction, "flour")
	assert.NotContains(t, recipeCritiqueSystemInstruction, "dough")
}

func TestRecipeCritiqueSystemInstructionKeepsTemperatureGuidanceConcise(t *testing.T) {
	for _, want := range []string{
		"recommend the doneness that best suits the dish with one concise target or pull temperature",
		"Careme's temperature guide as context",
		"intact beef or lamb 125-130 for medium-rare and 135-140 for medium",
		"pork loin or chops 140-145 and pork shoulder 195-205",
		"ground beef, pork, veal, or lamb 160",
		"all poultry 165 for safety, with breast pulled near 160 and rested to 165",
		"legs or thighs taken to 175-185",
		"salmon 125-130 and lean white fish 135-140",
		"egg dishes 160",
		"flag instructions to serve ground beef, pork, veal, or lamb below 160, poultry below 165 after any stated rest, or egg dishes below 160 as high-severity safety issues",
		"unless the recipe gives a validated time-at-temperature method that achieves equivalent safety",
		"do not use the preferred doneness ranges for intact beef, lamb, pork, or fish as automatic safety cutoffs",
		"evaluate temperature instructions in the context of the full cooking method, including time at temperature, carryover cooking, and whether the food is an intact or ground cut",
		"do not flag a temperature merely because it differs from a conventional or government-agency target",
		"Careme links a separate temperature guide beside the recipe",
		"treat it as a clarity issue and suggest a concise cooking instruction",
		"do not name the FDA, USDA, or other government agencies, quote official temperature guidance",
	} {
		assert.Contains(t, recipeCritiqueSystemInstruction, want)
	}
	assert.NotContains(t, recipeCritiqueSystemInstruction, "overall score below")
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
	assert.Equal(t, "integer", overallScore["type"])
	assert.NotContains(t, overallScore, "minimum")
	assert.NotContains(t, overallScore, "maximum")
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

func TestCritiquerReadyChecksCurrentOpenRouterKey(t *testing.T) {
	client := NewCritiquer("openrouter-key", "anthropic/claude-sonnet-4.5", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, "https", req.URL.Scheme)
			assert.Equal(t, "openrouter.ai", req.URL.Host)
			assert.Equal(t, "/api/v1/key", req.URL.Path)
			assert.Equal(t, "Bearer openrouter-key", req.Header.Get("Authorization"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":{"label":"openrouter-key"}}`)),
				Request:    req,
			}, nil
		}),
	})

	require.NoError(t, client.Ready(t.Context()))
}

func TestCritiquerReadyRejectsInvalidOpenRouterKey(t *testing.T) {
	client := NewCritiquer("invalid-key", "anthropic/claude-sonnet-4.5", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"code":401,"message":"Invalid API key"}}`)),
				Request:    req,
			}, nil
		}),
	})

	err := client.Ready(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check OpenRouter API key")
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
