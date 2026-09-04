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
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	MenuPlan ai.MenuPlan `json:"menu_plan"`
}

type recipeGenerator interface {
	GenerateRecipe(context.Context, []string, ai.ResponseRef) (*ai.Recipe, error)
}

type recipeCritiquer interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

type providerOptions struct {
	Config struct {
		Model      string `json:"model"`
		JudgeModel string `json:"judge_model"`
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
	if strings.TrimSpace(cfg.AI.APIKey) == "" || strings.TrimSpace(cfg.OpenRouter.APIKey) == "" {
		return nil, fmt.Errorf("AI_API_KEY and OPENROUTER_API_KEY are required for judged recipe generation evals")
	}
	generator := ai.NewClient(cfg.AI.APIKey, settings.Config.Model, http.DefaultClient, nil)
	judge := ai.NewCritiquer(cfg.OpenRouter.APIKey, settings.Config.JudgeModel, http.DefaultClient)
	result, err := runEval(body, generator, judge)
	if err != nil {
		return nil, err
	}
	result["metadata"].(map[string]interface{})["requestedModel"] = settings.Config.Model
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
	generated, err := generator.GenerateRecipe(context.Background(), instructions, pf.Vars.MenuPlan.ResponseRef())
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
	judgeStart := time.Now()
	critique, err := judge.CritiqueRecipe(context.Background(), result)
	judgeLatency := time.Since(judgeStart)
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
		"output":    string(output),
		"latencyMs": latency.Milliseconds(),
		"metadata": map[string]interface{}{
			"critique":       critique,
			"judgeLatencyMs": judgeLatency.Milliseconds(),
		},
	}, nil
}
