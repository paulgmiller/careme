package ai

import (
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
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

func TestNewClientUsesGPT56FamilyByRole(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, &capturePromptRecorder{})

	if client.model != config.DefaultRecipeModel {
		t.Fatalf("expected primary recipe model to be %q, got %q", config.DefaultRecipeModel, client.model)
	}
	if client.wineModel != gpt56Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}
