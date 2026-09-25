package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	Recipe ai.Recipe `json:"recipe"`
}

type imageGenerator interface {
	GenerateRecipeImage(context.Context, ai.Recipe, ai.RecipeImageStyle) (*ai.GeneratedImage, error)
}

type imageModels struct {
	Sketch string
	Photo  string
}

type latencyMeasurer func(context.Context, imageGenerator, ai.Recipe, ai.RecipeImageStyle) (time.Duration, error)

type latencyResult struct {
	Style     ai.RecipeImageStyle `json:"style"`
	Model     string              `json:"model"`
	LatencyMS int64               `json:"latency_ms"`
}

type comparisonResult struct {
	Sketch       latencyResult `json:"sketch"`
	Photo        latencyResult `json:"photo"`
	DifferenceMS int64         `json:"difference_ms"`
	PhotoRatio   float64       `json:"photo_to_sketch_ratio"`
}

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
	result, err := runEval(body, generator, imageModels{
		Sketch: string(cfg.AI.SketchImageModel),
		Photo:  string(cfg.AI.ImageModel),
	}, measureImageLatency)
	if err != nil {
		// Preserve errors in Promptfoo's Go wrapper, which otherwise hides them.
		return map[string]interface{}{"error": err.Error()}, nil
	}
	return result, nil
}

func runEval(body []byte, generator imageGenerator, models imageModels, measure latencyMeasurer) (map[string]interface{}, error) {
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

	// Run sketch first so the photo request gets any benefit from connection reuse.
	sketchLatency, err := measure(context.Background(), generator, pf.Vars.Recipe, ai.RecipeImageSketch)
	if err != nil {
		return nil, fmt.Errorf("measure sketch latency: %w", err)
	}
	photoLatency, err := measure(context.Background(), generator, pf.Vars.Recipe, ai.RecipeImagePhoto)
	if err != nil {
		return nil, fmt.Errorf("measure photo latency: %w", err)
	}

	sketchMS := sketchLatency.Milliseconds()
	photoMS := photoLatency.Milliseconds()
	ratio := 0.0
	if sketchLatency > 0 {
		ratio = float64(photoLatency) / float64(sketchLatency)
	}
	comparison := comparisonResult{
		Sketch:       latencyResult{Style: ai.RecipeImageSketch, Model: models.Sketch, LatencyMS: sketchMS},
		Photo:        latencyResult{Style: ai.RecipeImagePhoto, Model: models.Photo, LatencyMS: photoMS},
		DifferenceMS: photoMS - sketchMS,
		PhotoRatio:   ratio,
	}
	output, err := json.Marshal(comparison)
	if err != nil {
		return nil, fmt.Errorf("encode latency comparison: %w", err)
	}
	return map[string]interface{}{
		"output":    string(output),
		"latencyMs": (sketchLatency + photoLatency).Milliseconds(),
		"metadata": map[string]interface{}{
			"sketchLatencyMs": sketchMS,
			"photoLatencyMs":  photoMS,
			"differenceMs":    photoMS - sketchMS,
			"photoRatio":      ratio,
		},
	}, nil
}

func measureImageLatency(ctx context.Context, generator imageGenerator, recipe ai.Recipe, style ai.RecipeImageStyle) (time.Duration, error) {
	start := time.Now()
	image, err := generator.GenerateRecipeImage(ctx, recipe, style)
	latency := time.Since(start)
	if err != nil {
		return 0, err
	}
	if image == nil || image.Body == nil {
		return 0, fmt.Errorf("image generation returned no image body")
	}
	if _, err := io.Copy(io.Discard, image.Body); err != nil {
		return 0, fmt.Errorf("read generated image: %w", err)
	}
	return latency, nil
}
