package ai

import "testing"

func TestNewClientUsesGPT56FamilyByRole(t *testing.T) {
	client := NewClient("test-key", "", nil, &capturePromptRecorder{})

	if client.model != gpt56Sol {
		t.Fatalf("expected primary recipe model to be %q, got %q", gpt56Sol, client.model)
	}
	if client.wineModel != gpt56Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}

func TestNewClientUsesExplicitRecipeModel(t *testing.T) {
	client := NewClient("test-key", gpt56Terra, nil, &capturePromptRecorder{})

	if client.model != gpt56Terra {
		t.Fatalf("expected explicit recipe model to be %q, got %q", gpt56Terra, client.model)
	}
}

func TestNewClientTreatsPlaceholderModelAsDefault(t *testing.T) {
	client := NewClient("test-key", DefaultRecipeModel, nil, &capturePromptRecorder{})

	if client.model != gpt56Sol {
		t.Fatalf("expected placeholder model to default to %q, got %q", gpt56Sol, client.model)
	}
}
