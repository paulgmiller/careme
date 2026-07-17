package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
)

type evalScenario struct {
	Name         string                 `json:"name"`
	Location     locationtypes.Location `json:"location"`
	Date         string                 `json:"date"`
	Ingredients  []ai.InputIngredient   `json:"ingredients"`
	Directive    string                 `json:"directive"`
	Instructions string                 `json:"instructions"`
}

type menuPlanner interface {
	CreateMenuPlan(
		ctx context.Context,
		location *locationtypes.Location,
		ingredients []ai.InputIngredient,
		instructions []string,
		date time.Time,
		lastRecipes []string,
		count int,
	) (*ai.MenuPlan, error)
}

type recipeGenerator interface {
	GenerateRecipe(ctx context.Context, instructions []string, menuResponseID string) (*ai.Recipe, error)
}

func main() {
	logger := log.New(os.Stderr, "recipe eval provider: ", 0)
	if len(os.Args) < 2 {
		logger.Fatal("scenario JSON argument is required")
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("load configuration: %s", err)
	}
	if strings.TrimSpace(cfg.AI.APIKey) == "" {
		logger.Fatal("AI_API_KEY is required")
	}

	client := ai.NewClient(cfg.AI.APIKey, "", http.DefaultClient, nil)
	if err := run(context.Background(), os.Args[1], os.Stdout, client, client); err != nil {
		logger.Fatal(err)
	}
}

func run(ctx context.Context, rawScenario string, out io.Writer, planner menuPlanner, generator recipeGenerator) error {
	scenario, date, err := parseScenario(rawScenario)
	if err != nil {
		return err
	}

	plan, err := planner.CreateMenuPlan(
		ctx,
		&scenario.Location,
		scenario.Ingredients,
		compactStrings(scenario.Directive, scenario.Instructions),
		date,
		nil,
		1,
	)
	if err != nil {
		return fmt.Errorf("create menu plan for %q: %w", scenario.Name, err)
	}
	if plan == nil || len(plan.Plans) == 0 {
		return fmt.Errorf("create menu plan for %q: no plans returned", scenario.Name)
	}
	if strings.TrimSpace(plan.ResponseID) == "" {
		return fmt.Errorf("create menu plan for %q: response ID is required", scenario.Name)
	}

	recipeInstructions := append(compactStrings(scenario.Directive), plan.Plans[0].Instructions()...)
	recipe, err := generator.GenerateRecipe(ctx, recipeInstructions, plan.ResponseID)
	if err != nil {
		return fmt.Errorf("generate recipe for %q: %w", scenario.Name, err)
	}
	if recipe == nil {
		return fmt.Errorf("generate recipe for %q: empty recipe returned", scenario.Name)
	}

	if err := json.NewEncoder(out).Encode(recipe); err != nil {
		return fmt.Errorf("encode recipe for %q: %w", scenario.Name, err)
	}
	return nil
}

func parseScenario(raw string) (evalScenario, time.Time, error) {
	var scenario evalScenario
	if err := json.Unmarshal([]byte(raw), &scenario); err != nil {
		return evalScenario{}, time.Time{}, fmt.Errorf("decode scenario: %w", err)
	}
	scenario.Name = strings.TrimSpace(scenario.Name)
	if scenario.Name == "" {
		return evalScenario{}, time.Time{}, fmt.Errorf("scenario name is required")
	}
	if strings.TrimSpace(scenario.Location.State) == "" {
		return evalScenario{}, time.Time{}, fmt.Errorf("scenario %q location state is required", scenario.Name)
	}
	if len(scenario.Ingredients) == 0 {
		return evalScenario{}, time.Time{}, fmt.Errorf("scenario %q requires ingredients", scenario.Name)
	}
	for i, ingredient := range scenario.Ingredients {
		if strings.TrimSpace(ingredient.Description) == "" {
			return evalScenario{}, time.Time{}, fmt.Errorf("scenario %q ingredient %d description is required", scenario.Name, i+1)
		}
	}
	if strings.TrimSpace(scenario.Directive) == "" && strings.TrimSpace(scenario.Instructions) == "" {
		return evalScenario{}, time.Time{}, fmt.Errorf("scenario %q requires a directive or instructions", scenario.Name)
	}

	date, err := time.Parse(time.DateOnly, strings.TrimSpace(scenario.Date))
	if err != nil {
		return evalScenario{}, time.Time{}, fmt.Errorf("scenario %q date must use YYYY-MM-DD: %w", scenario.Name, err)
	}
	return scenario, date, nil
}

func compactStrings(values ...string) []string {
	compact := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			compact = append(compact, value)
		}
	}
	return compact
}
