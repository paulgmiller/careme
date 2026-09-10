package ai

import (
	"encoding/json"
	"net/http"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientTrimsModels(t *testing.T) {
	client := NewClient(config.AIConfig{
		APIKey:      "test-key",
		RecipeModel: " candidate-model ",
		ImageModel:  " candidate-image-model ",
	}, nil, &capturePromptRecorder{})

	assert.Equal(t, "candidate-model", client.model)
	assert.Equal(t, "candidate-image-model", string(client.imageModel))
	assert.Equal(t, defaultWineModel, client.wineModel)
}

func TestNewClientUsesModelsByRole(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, &capturePromptRecorder{})

	assert.Equal(t, "gpt-6-astra", client.model)
	if client.model != config.DefaultRecipeModel {
		t.Fatalf("expected primary recipe model to be %q, got %q", config.DefaultRecipeModel, client.model)
	}
	if client.wineModel != gpt56Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}

func TestResponseSpendForTier(t *testing.T) {
	standard := estimateOpenAIResponseSpend(config.DefaultRecipeModel, 1200, 900, 200, 350)
	assert.Equal(t, standard, responseSpendForTier(standard, "default"))
	assert.Equal(t, standard, responseSpendForTier(standard, ""))
	flex := responseSpendForTier(standard, "flex")
	assert.InDelta(t, standard.totalUSD()/2, flex.totalUSD(), 1e-9)
	assert.Equal(t, standard.cacheWriteUSD/2, flex.cacheWriteUSD)
	assert.Equal(t, standard.cachedInputUSD/2, flex.cachedInputUSD)
}

func TestFlexGenerationReturnsAPIErrorWithoutStandardFallback(t *testing.T) {
	aiConfig := testAIConfig(config.DefaultRecipeModel)
	aiConfig.ServiceTier = "flex"
	calls := 0
	c := NewClient(aiConfig, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "flex", body["service_tier"])
		resp := menuPlanHTTPResponse(req, "", "")
		resp.StatusCode = http.StatusBadRequest
		return resp, nil
	})}, nil)
	recipe, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
	require.ErrorContains(t, err, "failed to generate recipe")
	assert.Nil(t, recipe)
	assert.Equal(t, 1, calls)
}
