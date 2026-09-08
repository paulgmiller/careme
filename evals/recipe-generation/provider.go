package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	GenerateRecipe(context.Context, []string, ai.ResponseRef) (*ai.Recipe, error)
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
	generationUsage := &usageTransport{next: http.DefaultTransport}
	judgeUsage := &usageTransport{next: http.DefaultTransport, judge: true}
	generator := ai.NewClient(cfg.AI.APIKey, settings.Config.Model, &http.Client{Transport: generationUsage}, nil).WithRecipeReasoningEffort(settings.Config.ReasoningEffort)
	judge := ai.NewCritiquer(cfg.OpenRouter.APIKey, settings.Config.JudgeModel, &http.Client{Transport: judgeUsage})
	result, err := runEval(body, generator, judge)
	if err != nil {
		return nil, err
	}
	if generationUsage.err != nil {
		return nil, fmt.Errorf("generation cost: %w", generationUsage.err)
	}
	if judgeUsage.err != nil {
		return nil, fmt.Errorf("judge cost: %w", judgeUsage.err)
	}
	// Promptfoo's cost column compares generation, like its latency column.
	result["cost"] = generationUsage.usage.CostUSD
	result["tokenUsage"] = map[string]int64{
		"prompt":     generationUsage.usage.InputTokens,
		"completion": generationUsage.usage.OutputTokens,
		"cached":     generationUsage.usage.CachedInputTokens,
		"total":      generationUsage.usage.InputTokens + generationUsage.usage.OutputTokens,
	}
	metadata := result["metadata"].(map[string]interface{})
	metadata["generationUsage"] = generationUsage.usage
	metadata["judgeUsage"] = judgeUsage.usage
	metadata["totalCostUSD"] = generationUsage.usage.CostUSD + judgeUsage.usage.CostUSD
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

// usageTransport observes the non-streaming eval calls without putting usage
// fields into generated recipes or the judge's input. Each eval owns its meters.
type usageTransport struct {
	next  http.RoundTripper
	judge bool
	usage callUsage
	err   error
}

type callUsage struct {
	CostUSD           float64 `json:"costUSD"`
	InputTokens       int64   `json:"inputTokens"`
	CachedInputTokens int64   `json:"cachedInputTokens"`
	CacheWriteTokens  int64   `json:"cacheWriteTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	ReasoningTokens   int64   `json:"reasoningTokens"`
}

func (t *usageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read eval API response: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	usage, err := decodeCallUsage(body, t.judge)
	if err != nil {
		// Preserve the response for the SDK, and fail accounting after the call.
		// Returning a transport error here could retry an already completed paid call.
		t.err = err
	} else {
		t.usage.CostUSD += usage.CostUSD
		t.usage.InputTokens += usage.InputTokens
		t.usage.CachedInputTokens += usage.CachedInputTokens
		t.usage.CacheWriteTokens += usage.CacheWriteTokens
		t.usage.OutputTokens += usage.OutputTokens
		t.usage.ReasoningTokens += usage.ReasoningTokens
	}
	return resp, nil
}

func decodeCallUsage(body []byte, judge bool) (callUsage, error) {
	var resp struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			InputDetails struct {
				Cached     int64 `json:"cached_tokens"`
				CacheWrite int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				Reasoning int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			PromptDetails    struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionDetails struct {
				Reasoning int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
			Cost *float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return callUsage{}, fmt.Errorf("decode eval usage: %w", err)
	}
	if resp.Usage == nil {
		return callUsage{}, fmt.Errorf("API response omitted usage required for eval cost")
	}
	u := resp.Usage
	if judge {
		if u.Cost == nil || *u.Cost < 0 || math.IsNaN(*u.Cost) || math.IsInf(*u.Cost, 0) {
			return callUsage{}, fmt.Errorf("judge response omitted valid usage.cost required for eval cost")
		}
		return callUsage{CostUSD: *u.Cost, InputTokens: u.PromptTokens, CachedInputTokens: u.PromptDetails.Cached, OutputTokens: u.CompletionTokens, ReasoningTokens: u.CompletionDetails.Reasoning}, nil
	}
	cost, err := ai.EstimateResponseCostUSD(resp.Model, u.InputTokens, u.InputDetails.Cached, u.InputDetails.CacheWrite, u.OutputTokens)
	if err != nil {
		return callUsage{}, err
	}
	return callUsage{CostUSD: cost, InputTokens: u.InputTokens, CachedInputTokens: u.InputDetails.Cached, CacheWriteTokens: u.InputDetails.CacheWrite, OutputTokens: u.OutputTokens, ReasoningTokens: u.OutputDetails.Reasoning}, nil
}
