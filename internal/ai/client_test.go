package ai

import (
	"testing"

	openai "github.com/openai/openai-go/v3"
)

func TestNewClientUsesGPT55ForRecipeFlow(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, &capturePromptRecorder{})

	if client.model != "gpt-5.5" {
		t.Fatalf("expected primary recipe model to be gpt-5.5, got %q", client.model)
	}
	if client.wineModel != openai.ChatModelGPT5Mini {
		t.Fatalf("expected wine model to remain low-cost mini path, got %q", client.wineModel)
	}
}
