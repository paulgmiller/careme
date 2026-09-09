package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"

	"github.com/openai/openai-go/v3/responses"
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	MenuPlan ai.MenuPlan `json:"menu_plan"`
}

type recipeGenerator interface {
	GenerateRecipeWithCost(context.Context, []string, ai.ResponseRef) (*ai.Recipe, float64, error)
}

type recipeCritiquer interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

type providerOptions struct {
	Config struct {
		Model           string                    `json:"model"`
		ReasoningEffort responses.ReasoningEffort `json:"reasoning_effort"`
		JudgeModel      string                    `json:"judge_model"`
	} `json:"config"`
}

func CallApi(_ string, options map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	result, err := callAPI(options, ctx)
	if err != nil {
		// Preserve errors in Promptfoo's Go wrapper, which otherwise hides them.
		return map[string]interface{}{"error": err.Error()}, nil
	}
	return result, nil
}

func callAPI(options map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Promptfoo context: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	settings, err := decodeOptions(options)
	if err != nil {
		return nil, err
	}
	if settings.Config.Model == "" {
		settings.Config.Model = cfg.AI.RecipeModel
	}
	if settings.Config.JudgeModel == "" {
		settings.Config.JudgeModel = cfg.OpenRouter.CritiqueModel
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" || strings.TrimSpace(cfg.OpenRouter.APIKey) == "" {
		return nil, fmt.Errorf("AI_API_KEY and OPENROUTER_API_KEY are required for judged recipe generation evals")
	}
	aiConfig := cfg.AI
	aiConfig.RecipeModel = settings.Config.Model
	generator := ai.NewClient(aiConfig, http.DefaultClient, nil).WithRecipeReasoningEffort(settings.Config.ReasoningEffort)
	judge := ai.NewCritiquer(cfg.OpenRouter.APIKey, settings.Config.JudgeModel, http.DefaultClient)
	result, err := runEval(body, generator, judge)
	if err != nil {
		return nil, err
	}
	metadata := result["metadata"].(map[string]interface{})
	metadata["requestedModel"] = settings.Config.Model
	metadata["requestedReasoningEffort"] = settings.Config.ReasoningEffort
	return result, nil
}

func decodeOptions(options map[string]interface{}) (providerOptions, error) {
	var settings providerOptions
	body, err := json.Marshal(options)
	if err != nil {
		return settings, fmt.Errorf("encode provider options: %w", err)
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return settings, fmt.Errorf("decode provider options: %w", err)
	}
	settings.Config.Model = strings.TrimSpace(settings.Config.Model)
	if settings.Config.Model == "" {
		settings.Config.Model = strings.TrimSpace(os.Getenv("RECIPE_EVAL_MODEL"))
	}
	settings.Config.ReasoningEffort = responses.ReasoningEffort(strings.TrimSpace(string(settings.Config.ReasoningEffort)))
	if settings.Config.ReasoningEffort == "" {
		settings.Config.ReasoningEffort = responses.ReasoningEffort(strings.TrimSpace(os.Getenv("RECIPE_EVAL_REASONING_EFFORT")))
	}
	switch settings.Config.ReasoningEffort {
	case "", responses.ReasoningEffortNone, responses.ReasoningEffortMinimal, responses.ReasoningEffortLow, responses.ReasoningEffortMedium, responses.ReasoningEffortHigh, responses.ReasoningEffortXhigh, responses.ReasoningEffortMax:
	default:
		return settings, fmt.Errorf("invalid recipe reasoning effort %q: use none, minimal, low, medium, high, xhigh, or max", settings.Config.ReasoningEffort)
	}
	return settings, nil
}

func runEval(body []byte, generator recipeGenerator, judge recipeCritiquer) (map[string]interface{}, error) {
	var pf promptfooContext
	if err := json.Unmarshal(body, &pf); err != nil {
		return nil, fmt.Errorf("failed to decode Promptfoo context: %w", err)
	}

	if len(pf.Vars.MenuPlan.Plans) != 1 {
		return nil, fmt.Errorf("eval menu plan must contain exactly one recipe plan")
	}
	if strings.TrimSpace(pf.Vars.MenuPlan.ResponseID) == "" {
		return nil, fmt.Errorf("eval menu plan response id is required")
	}

	instructions := pf.Vars.MenuPlan.Plans[0].Instructions()
	start := time.Now()
	generated, generationCost, err := generator.GenerateRecipeWithCost(context.Background(), instructions, pf.Vars.MenuPlan.ResponseRef())
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipe: %w", err)
	}
	if generated == nil {
		return nil, fmt.Errorf("failed to generate recipe: AI returned no recipe")
	}

	result := *generated
	result.ResponseID = ""
	result.PromptCacheKey = ""
	result.OriginHash = ""
	result.ParentHash = ""
	critique, err := judge.CritiqueRecipe(context.Background(), result)
	if err != nil {
		return nil, fmt.Errorf("judge generated recipe: %w", err)
	}
	if critique == nil {
		return nil, fmt.Errorf("judge returned no critique")
	}
	output, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to encode recipe: %w", err)
	}
	return map[string]interface{}{
		"output": string(output),
		// Promptfoo's cost column compares generation, like its latency column.
		"cost":      generationCost,
		"latencyMs": latency.Milliseconds(),
		"metadata": map[string]interface{}{
			"critique": critique,
		},
	}, nil
}
