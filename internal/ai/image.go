package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strings"

	openai "github.com/openai/openai-go/v3"
)

type GeneratedImage struct {
	Body io.Reader
}

const recipeImagePromptInstructions = `
Generate a realistic overhead food photograph of a single finished plate.
- Home cooked by a above average cook, not a restaurant or food stylist.
- Keep plating simple and believable. No tweezers, foam, edible flowers, microgreens, or luxury flourishes unless in recipe instructions.
- Use a simple kitchen counter, stovetop, sheet pan, wooden table, or casual dining table backdrop.
- Use natural colors, ordinary cookware or tableware, and realistic portions
- Avoid text, labels, branded packaging, people, hands, collages, and extra side dishes
- If the recipe has multiple components, show them plated together
`

const (
	recipeImageModel = openai.ImageModelGPTImage2 // dalle-3 is getting deprecated. 1.5 seems way better than 1.
	// WebP is materially smaller for these recipe photos on mobile, and GPT image models support direct WebP output.
	recipeImageOutputFormat = openai.ImageGenerateParamsOutputFormatWebP
	recipeImageQuality      = openai.ImageGenerateParamsQualityMedium
	recipeImageSize         = openai.ImageGenerateParamsSize1024x1024
)

func (c *client) GenerateRecipeImage(ctx context.Context, recipe Recipe) (*GeneratedImage, error) {
	prompt, err := buildRecipeImagePrompt(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe image prompt: %w", err)
	}

	resp, err := c.oai.Images.Generate(ctx, openai.ImageGenerateParams{
		Prompt:       prompt,
		Model:        recipeImageModel,
		N:            openai.Int(1),
		OutputFormat: recipeImageOutputFormat,
		Quality:      recipeImageQuality,
		Size:         recipeImageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipe image: %w", err)
	}

	slog.InfoContext(ctx, "API usage", "model", string(recipeImageModel), imageUsageLogAttr(string(recipeImageModel), resp.Usage))
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("image generation returned no images")
	}
	imageBody := strings.TrimSpace(resp.Data[0].B64JSON)
	if imageBody == "" {
		return nil, fmt.Errorf("image generation returned empty image data")
	}

	return &GeneratedImage{
		Body: base64.NewDecoder(base64.StdEncoding, strings.NewReader(imageBody)),
	}, nil
}

func imageUsageLogAttr(model string, usage openai.ImagesResponseUsage) slog.Attr {
	return slog.Group("usage",
		slog.Int64("inputTokens", usage.InputTokens),
		slog.Group("inputTokensDetails",
			slog.Int64("imageTokens", usage.InputTokensDetails.ImageTokens),
			slog.Int64("textTokens", usage.InputTokensDetails.TextTokens),
		),
		slog.Int64("outputTokens", usage.OutputTokens),
		slog.Group("outputTokensDetails",
			slog.Int64("imageTokens", usage.OutputTokensDetails.ImageTokens),
			slog.Int64("textTokens", usage.OutputTokensDetails.TextTokens),
		),
		slog.Int64("totalTokens", usage.TotalTokens),
		estimatedSpendLogAttr(estimateOpenAIImageSpend(
			model,
			usage.InputTokensDetails.TextTokens,
			usage.InputTokensDetails.ImageTokens,
			usage.OutputTokens,
		)),
	)
}

func buildRecipeImagePrompt(recipe Recipe) (string, error) {
	var promptBuilder strings.Builder
	fmt.Fprintf(&promptBuilder, "%s\n", recipeImagePromptInstructions)
	fmt.Fprintf(&promptBuilder, "\n")
	fmt.Fprintf(&promptBuilder, "Recipe:\n")
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Title)
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Description)
	fmt.Fprintf(&promptBuilder, "Instructions:\n")
	for _, ins := range recipe.Instructions {
		fmt.Fprintf(&promptBuilder, "- %s\n", ins)
	}
	return promptBuilder.String(), nil
}
