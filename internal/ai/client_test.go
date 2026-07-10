package ai

import "testing"

func TestNewClientUsesGPT56FamilyByRole(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, &capturePromptRecorder{})

	if client.model != gpt56Sol {
		t.Fatalf("expected primary recipe model to be %q, got %q", gpt56Sol, client.model)
	}
	if client.wineModel != gpt56Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}
