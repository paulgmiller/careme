package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/config"
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	Recipe     ai.Recipe           `json:"recipe"`
	ImageStyle ai.RecipeImageStyle `json:"image_style"` // photo or sketch
}

type imageGenerator interface {
	GenerateRecipeImage(context.Context, ai.Recipe, ai.RecipeImageStyle) (*ai.GeneratedImage, error)
}

type latencyMeasurer func(context.Context, imageGenerator, ai.Recipe, ai.RecipeImageStyle) error

func CallApi(_ string, _ map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("failed to encode Promptfoo context: %v", err)}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("failed to load configuration: %v", err)}, nil
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" {
		return map[string]interface{}{"error": "AI_API_KEY is required for image latency evals"}, nil
	}

	generator := ai.NewClient(cfg.AI, http.DefaultClient, nil)
	result, err := runEval(body, generator, measureImageLatency)
	if err != nil {
		// Preserve errors in Promptfoo's Go wrapper, which otherwise hides them.
		return map[string]interface{}{"error": err.Error()}, nil
	}
	return result, nil
}

func runEval(body []byte, generator imageGenerator, measure latencyMeasurer) (map[string]interface{}, error) {
	var pf promptfooContext
	if err := json.Unmarshal(body, &pf); err != nil {
		return nil, fmt.Errorf("failed to decode Promptfoo context: %w", err)
	}
	if strings.TrimSpace(pf.Vars.Recipe.Title) == "" {
		return nil, fmt.Errorf("eval recipe title is required")
	}
	if len(pf.Vars.Recipe.Instructions) == 0 {
		return nil, fmt.Errorf("at least one eval recipe instruction is required")
	}
	if pf.Vars.ImageStyle != ai.RecipeImageSketch && pf.Vars.ImageStyle != ai.RecipeImagePhoto {
		return nil, fmt.Errorf("eval image_style must be photo or sketch")
	}

	err := measure(context.Background(), generator, pf.Vars.Recipe, pf.Vars.ImageStyle)
	if err != nil {
		return nil, fmt.Errorf("measure %s latency: %w", pf.Vars.ImageStyle, err)
	}
	return map[string]interface{}{
		"output": string(pf.Vars.ImageStyle),
	}, nil
}

// TODO should we measure dimensions? assert webp? let another ai analyze the image?
func measureImageLatency(ctx context.Context, generator imageGenerator, recipe ai.Recipe, style ai.RecipeImageStyle) error {
	image, err := generator.GenerateRecipeImage(ctx, recipe, style)
	if err != nil {
		return err
	}
	if image == nil || image.Body == nil {
		return fmt.Errorf("image generation returned no image body")
	}
	if _, err := io.Copy(io.Discard, image.Body); err != nil {
		return fmt.Errorf("read generated image: %w", err)
	}
	return nil
}
