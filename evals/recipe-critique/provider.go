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
	"careme/internal/cache"
	"careme/internal/recipes"

	"github.com/paulgmiller/kage/pkg/kage"
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	Recipe     *ai.Recipe `json:"recipe"`
	RecipeHash string     `json:"recipe_hash"`
}

type recipeCritiquer interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

type recipeLoader interface {
	SingleFromCache(context.Context, string) (*ai.Recipe, error)
}

func CallApi(_ string, _ map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	result, err := callAPI(ctx)
	if err != nil {
		// Promptfoo's generated Go wrapper exits without exposing the error text
		// when CallApi returns an error. ProviderResponse.error keeps it visible.
		return map[string]interface{}{"error": err.Error()}, nil
	}
	return result, nil
}

func callAPI(ctx map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Promptfoo context: %w", err)
	}
	if err := kage.Load(); err != nil {
		return nil, fmt.Errorf("load encrypted environment: %w", err)
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is required for recipe critique evals")
	}
	critiquer := ai.NewCritiquer(apiKey, os.Getenv("OPENROUTER_CRITIQUE_MODEL"), http.DefaultClient)

	testCase, err := decodeEvalCase(body)
	if err != nil {
		return nil, err
	}
	if testCase.Recipe != nil {
		return critiqueRecipe(context.Background(), *testCase.Recipe, critiquer)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("open recipe cache: %w", err)
	}
	return runEval(context.Background(), testCase, recipes.IO(cacheStore), critiquer)
}

func decodeEvalCase(body []byte) (evalCase, error) {
	var pf promptfooContext
	if err := json.Unmarshal(body, &pf); err != nil {
		return evalCase{}, fmt.Errorf("failed to decode Promptfoo context: %w", err)
	}
	pf.Vars.RecipeHash = strings.TrimSpace(pf.Vars.RecipeHash)
	if (pf.Vars.Recipe == nil) == (pf.Vars.RecipeHash == "") {
		return evalCase{}, fmt.Errorf("eval must provide exactly one of recipe or recipe_hash")
	}
	return pf.Vars, nil
}

func runEval(ctx context.Context, testCase evalCase, loader recipeLoader, critiquer recipeCritiquer) (map[string]interface{}, error) {
	recipe := testCase.Recipe
	if recipe == nil {
		loaded, err := loader.SingleFromCache(ctx, testCase.RecipeHash)
		if err != nil {
			return nil, fmt.Errorf("load recipe %s: %w", testCase.RecipeHash, err)
		}
		recipe = loaded
	}
	if recipe == nil {
		return nil, fmt.Errorf("load recipe %s: cache returned no recipe", testCase.RecipeHash)
	}
	return critiqueRecipe(ctx, *recipe, critiquer)
}

func critiqueRecipe(ctx context.Context, recipe ai.Recipe, critiquer recipeCritiquer) (map[string]interface{}, error) {
	start := time.Now()
	critique, err := critiquer.CritiqueRecipe(ctx, recipe)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("critique recipe %q: %w", recipe.Title, err)
	}
	if critique == nil {
		return nil, fmt.Errorf("critique recipe %q: model returned no critique", recipe.Title)
	}
	output, err := json.Marshal(critique)
	if err != nil {
		return nil, fmt.Errorf("encode recipe critique: %w", err)
	}
	return map[string]interface{}{
		"output":    string(output),
		"latencyMs": latency.Milliseconds(),
	}, nil
}
