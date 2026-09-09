package ai

import (
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"careme/internal/config"

	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGenerateRecipeImageUsesConfiguredModel(t *testing.T) {
	const imageModel = "gpt-image-2.5-sunburst"
	aiConfig := testAIConfig(config.DefaultRecipeModel)
	aiConfig.ImageModel = imageModel
	client := NewClient(aiConfig, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"model":"`+imageModel+`"`)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"created":1,"data":[{"b64_json":"aW1hZ2U="}]}`)),
			Request:    req,
		}, nil
	})}, nil)

	image, err := client.GenerateRecipeImage(t.Context(), Recipe{Title: "Soup"})
	require.NoError(t, err)
	imageBody, err := io.ReadAll(image.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("image"), imageBody)
}

func TestImageUsageLogAttr(t *testing.T) {
	attr := imageUsageLogAttr(config.DefaultImageModel, openai.ImagesResponseUsage{
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
		slog.Group("spend",
			slog.String("currency", "USD"),
			slog.Float64("totalUSD", 0.00668),
			slog.Float64("inputUSD", 0.00068),
			slog.Float64("cachedInputUSD", 0),
			slog.Float64("cacheWriteInputUSD", 0),
			slog.Float64("outputUSD", 0.006),
		),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}

func TestEstimateOpenAIImageSpendSupportsGPTImage25Models(t *testing.T) {
	for _, model := range []string{config.DefaultImageModel, "gpt-image-2.5-sunburst"} {
		t.Run(model, func(t *testing.T) {
			spend := estimateOpenAIImageSpend(model, 40, 60, 200)

			if spend.reason != "" {
				t.Fatalf("expected pricing for %s, got reason %q", model, spend.reason)
			}
			assert.InDelta(t, 0.00668, spend.totalUSD(), 0.000000001)
		})
	}
}
