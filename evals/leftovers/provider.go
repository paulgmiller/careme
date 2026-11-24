package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"

	"github.com/openai/openai-go/v3/responses"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

//unexport:ignore Promptfoo loads the CallApi symbol dynamically
func CallApi(_ string, options map[string]any, ctx map[string]any) (map[string]any, error) {
	result, err := callAPI(options, ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

func callAPI(options map[string]any, ctx map[string]any) (map[string]any, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" {
		return nil, fmt.Errorf("AI_API_KEY is required for leftovers evals")
	}
	var settings struct {
		Config struct {
			ServiceTier responses.ResponseNewParamsServiceTier `json:"service_tier"`
		} `json:"config"`
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	cfg.AI.ServiceTier = settings.Config.ServiceTier
	var input struct {
		Vars struct {
			Input ai.LeftoversInput `json:"input"`
		} `json:"vars"`
	}
	raw, err = json.Marshal(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	client := ai.NewClient(cfg.AI, &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}, nil)
	start := time.Now()
	questions, err := client.SuggestLeftoversIngredients(context.Background(), input.Vars.Input)
	if err != nil {
		return nil, err
	}
	input.Vars.Input.Asked = append(input.Vars.Input.Asked, questions...)
	result, err := client.GenerateLeftoversMeal(context.Background(), input.Vars.Input)
	if err != nil {
		return nil, err
	}
	// Provider response IDs are runtime metadata, not eval fixtures or outputs.
	for i := range result.Recipes {
		result.Recipes[i].ResponseID = ""
	}
	output, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": string(output), "latencyMs": time.Since(start).Milliseconds(), "metadata": map[string]any{"pantryQuestions": questions, "requestedServiceTier": settings.Config.ServiceTier}}, nil
}
