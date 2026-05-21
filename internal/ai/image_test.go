package ai

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

func TestBuildRecipeImagePrompt(t *testing.T) {
	recipe := Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []Ingredient{{Name: "Chicken", Quantity: "1 whole"}},
		Instructions: []string{"Roast until golden."},
	}

	prompt, err := buildRecipeImagePrompt(recipe)
	if err != nil {
		t.Fatalf("buildRecipeImagePrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "realistic overhead food photograph") {
		t.Fatalf("expected image prompt instructions in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Recipe:\nRoast Chicken\nCrisp skin and herbs.\nInstructions:\n- Roast until golden.\n") {
		t.Fatalf("expected recipe summary in prompt: %s", prompt)
	}
}

func TestImageUsageLogAttr(t *testing.T) {
	attr := imageUsageLogAttr(openai.ImagesResponseUsage{
		InputTokens:  100,
		OutputTokens: 200,
		TotalTokens:  300,
		InputTokensDetails: openai.ImagesResponseUsageInputTokensDetails{
			ImageTokens: 60,
			TextTokens:  40,
		},
		OutputTokensDetails: openai.ImagesResponseUsageOutputTokensDetails{
			ImageTokens: 180,
			TextTokens:  20,
		},
	})

	if attr.Key != "usage" {
		t.Fatalf("unexpected attr key: %s", attr.Key)
	}
	if attr.Value.Kind() != slog.KindGroup {
		t.Fatalf("unexpected attr kind: %v", attr.Value.Kind())
	}
	if !reflect.DeepEqual(attr.Value.Group(), []slog.Attr{
		slog.Int64("inputTokens", 100),
		slog.Group("inputTokensDetails",
			slog.Int64("imageTokens", 60),
			slog.Int64("textTokens", 40),
		),
		slog.Int64("outputTokens", 200),
		slog.Group("outputTokensDetails",
			slog.Int64("imageTokens", 180),
			slog.Int64("textTokens", 20),
		),
		slog.Int64("totalTokens", 300),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}
