package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRecipeWithCostFailsWithoutAccounting(t *testing.T) {
	for _, tc := range []struct {
		name, model, usage, wantErr string
	}{
		{name: "missing usage", model: "gpt-6-astra", usage: `null`, wantErr: "invalid token usage"},
		{name: "unknown model", model: "unpriced-model", usage: `{"input_tokens":10,"output_tokens":5}`, wantErr: "price_not_configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := NewClient(testAIConfig(tc.model), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				body, err := json.Marshal(map[string]any{
					"id": "resp-recipe", "model": tc.model,
					"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": `{"title":"Dinner"}`}}}},
					"usage":  json.RawMessage(tc.usage),
				})
				require.NoError(t, err)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})}, noopPromptRecorder{})
			recipe, cost, err := client.GenerateRecipeWithCost(t.Context(), []string{"Dinner"}, ResponseRef{ID: "menu"})
			require.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, recipe)
			assert.Zero(t, cost)
			assert.Equal(t, 1, calls, "accounting errors must not retry a completed paid call")

			// Ordinary recipe generation does not require pricing to be configured.
			recipe, err = client.GenerateRecipe(t.Context(), []string{"Dinner"}, ResponseRef{ID: "menu"})
			require.NoError(t, err)
			assert.Equal(t, "Dinner", recipe.Title)
		})
	}
}

func TestCritiqueRecipeWithCostRequiresReportedCost(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		wantErr     bool
	}{
		{name: "zero cost", usage: `{"cost":0}`},
		{name: "missing cost", usage: `{}`, wantErr: true},
		{name: "missing usage", usage: `null`, wantErr: true},
		{name: "negative cost", usage: `{"cost":-1}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := NewCritiquer("test-key", "judge", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				body, err := json.Marshal(map[string]any{
					"id": "critique", "model": "judge",
					"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"overall_score":8,"summary":"Good dinner."}`}}},
					"usage":   json.RawMessage(tc.usage),
				})
				require.NoError(t, err)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
			})})
			critique, cost, err := client.CritiqueRecipeWithCost(t.Context(), Recipe{Title: "Dinner"})
			assert.Equal(t, 1, calls)
			assert.Zero(t, cost)
			if tc.wantErr {
				require.ErrorContains(t, err, "usage.cost")
				assert.Nil(t, critique)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 8, critique.OverallScore)
			}
			critique, err = client.CritiqueRecipe(t.Context(), Recipe{Title: "Dinner"})
			require.NoError(t, err)
			assert.Equal(t, 8, critique.OverallScore)
		})
	}
}
