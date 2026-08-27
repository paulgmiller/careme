package recipeeval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

func CallApi(_ string, _ map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode Promptfoo context: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	generator := ai.NewClient(cfg.AI.APIKey, "", http.DefaultClient, nil)
	return runEval(body, generator)
}

func runEval(body []byte, generator recipeGenerator) (map[string]interface{}, error) {
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
	generated, err := generator.GenerateRecipe(context.Background(), instructions, pf.Vars.MenuPlan.ResponseRef())
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipe: %w", err)
	}
	if generated == nil {
		return nil, fmt.Errorf("failed to generate recipe: AI returned no recipe")
	}

	result := *generated
	result.ResponseID = ""
	result.PromptCacheKey = ""
	output, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to encode recipe: %w", err)
	}
	return map[string]interface{}{"output": string(output)}, nil
}
