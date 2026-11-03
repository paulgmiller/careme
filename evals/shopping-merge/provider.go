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
	"careme/internal/recipes"
)

type shoppingMerger interface {
	MergeShoppingQuantities(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error)
}

//unexport:ignore Promptfoo loads the CallApi symbol dynamically
func CallApi(_ string, _ map[string]any, ctx map[string]any) (map[string]any, error) {
	result, err := callAPI(ctx)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return result, nil
}

func callAPI(ctx map[string]any) (map[string]any, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" {
		return nil, fmt.Errorf("AI_API_KEY is required for shopping merge evals")
	}
	return runEval(ctx, ai.NewClient(cfg.AI, http.DefaultClient, nil))
}

func runEval(ctx map[string]any, merger shoppingMerger) (map[string]any, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("encode eval context: %w", err)
	}
	var input struct {
		Vars struct {
			Ingredients []ai.Ingredient      `json:"ingredients"`
			Catalog     []ai.InputIngredient `json:"catalog"`
		} `json:"vars"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, fmt.Errorf("decode eval context: %w", err)
	}
	if len(input.Vars.Ingredients) == 0 {
		return nil, fmt.Errorf("eval ingredients are required")
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := time.Now()
	items, err := merger.MergeShoppingQuantities(requestCtx, input.Vars.Ingredients, recipes.MatchingShoppingCatalog(input.Vars.Ingredients, input.Vars.Catalog))
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("merge shopping ingredients: %w", err)
	}
	output, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode shopping items: %w", err)
	}
	return map[string]any{"output": string(output), "latencyMs": latency.Milliseconds()}, nil
}
