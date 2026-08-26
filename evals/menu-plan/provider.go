package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
)

type promptfooContext struct {
	Vars evalCase `json:"vars"`
}

type evalCase struct {
	Location     locations.Location   `json:"location"`
	Ingredients  []ai.InputIngredient `json:"ingredients"`
	Instructions string               `json:"instructions,omitempty"`
	Date         string               `json:"date"`
	LastRecipes  []string             `json:"last_recipes,omitempty"`
	Count        int                  `json:"count,omitempty,omitzero"`
}

type menuPlanner interface {
	CreateMenuPlan(context.Context, *locations.Location, []ai.InputIngredient, []string, time.Time, []string, int) (*ai.MenuPlan, error)
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
	planner := ai.NewClient(cfg.AI.APIKey, "", http.DefaultClient, nil)
	return runEval(body, planner)
}

func runEval(body []byte, planner menuPlanner) (map[string]interface{}, error) {
	testCase, err := decodeEvalCase(body)
	if err != nil {
		return nil, err
	}
	date, err := time.Parse(time.DateOnly, strings.TrimSpace(testCase.Date))
	if err != nil {
		return nil, fmt.Errorf("invalid eval date %q: expected YYYY-MM-DD: %w", testCase.Date, err)
	}

	plan, err := planner.CreateMenuPlan(
		context.Background(),
		&testCase.Location,
		testCase.Ingredients,
		[]string{testCase.Instructions},
		date,
		testCase.LastRecipes,
		testCase.Count,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create menu plan: %w", err)
	}
	if plan == nil {
		return nil, fmt.Errorf("failed to create menu plan: AI returned nil plan")
	}

	// Response metadata belongs to the provider's continuation plumbing, not the
	// model output Promptfoo evaluates.
	result := *plan
	result.ResponseID = ""
	result.PromptCacheKey = ""
	output, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to encode menu plan: %w", err)
	}
	return map[string]interface{}{"output": string(output)}, nil
}

func decodeEvalCase(body []byte) (evalCase, error) {
	var pf promptfooContext
	if err := json.Unmarshal(body, &pf); err != nil {
		return evalCase{}, fmt.Errorf("failed to decode Promptfoo context: %w", err)
	}
	if strings.TrimSpace(pf.Vars.Location.ID) == "" {
		return evalCase{}, fmt.Errorf("eval location id is required")
	}
	if len(pf.Vars.Ingredients) == 0 {
		return evalCase{}, fmt.Errorf("at least one eval ingredient is required")
	}
	if pf.Vars.Count == 0 {
		pf.Vars.Count = 1
	}

	return pf.Vars, nil
}
