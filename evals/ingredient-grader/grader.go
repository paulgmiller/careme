package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ingredients/grading"
)

type expectation struct {
	Min int `json:"min,omitempty"`
	Max int `json:"max,omitempty"`
}

type promptfooContext struct {
	Vars struct {
		Cases []EvalCase `json:"cases"`
	} `json:"vars"`
}
type EvalCase struct {
	// only expecting Brand, Descirption and maybe size from hard coded entries.
	Ingredient ai.InputIngredient `json:"ingredient"`
	Expect     expectation        `json:"expect"`
}

type ingredientGrader interface {
	GradeIngredients(context.Context, []ai.InputIngredient) ([]ai.InputIngredient, error)
}

type providerOptions struct {
	Config struct {
		Model string `json:"model"`
	} `json:"config"`
}

func CallApi(_ string, options map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	result, err := callAPI(options, ctx)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}, nil
	}
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
		settings.Config.Model = strings.TrimSpace(os.Getenv("INGREDIENT_EVAL_MODEL"))
	}
	return settings, nil
}

func callAPI(options map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %s", err)
	}
	settings, err := decodeOptions(options)
	if err != nil {
		return nil, err
	}
	if settings.Config.Model != "" {
		cfg.IngredientGrading.Model = settings.Config.Model
	}
	if cfg.IngredientGrading.Model != "jev" && strings.TrimSpace(cfg.AI.APIKey) == "" {
		return nil, fmt.Errorf("AI_API_KEY is required for ingredient grading evals")
	}
	// Every evaluation must grade fresh inputs, regardless of production settings
	// or previously stored grades. Keep the production batching path.
	cfg.IngredientGrading.Enable = true
	grader := grading.NewManager(cfg, cache.NewInMemoryCache(), http.DefaultClient)
	result, err := runEval(ctx, grader)
	if err != nil {
		return nil, err
	}
	result["metadata"].(map[string]interface{})["requestedModel"] = cfg.IngredientGrading.Model
	return result, nil
}

func runEval(ctx map[string]interface{}, grader ingredientGrader) (map[string]interface{}, error) {
	var pf promptfooContext
	b, err := json.Marshal(ctx)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, err
	}
	if len(pf.Vars.Cases) == 0 {
		return nil, fmt.Errorf("at least one ingredient eval case is required")
	}

	var ings []ai.InputIngredient
	expectations := map[string]expectation{}
	for i, eval := range pf.Vars.Cases {
		ing := eval.Ingredient
		if ing.ProductID == "" {
			ing.ProductID = strconv.Itoa(i)
		}

		if eval.Expect.Max == 0 {
			eval.Expect.Max = 10
		}
		ings = append(ings, ing)
		if _, exists := expectations[ing.ProductID]; exists {
			return nil, fmt.Errorf("duplicate eval product id %q", ing.ProductID)
		}
		expectations[ing.ProductID] = eval.Expect
	}

	start := time.Now()
	grades, err := grader.GradeIngredients(context.Background(), ings)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("failed to grade ingredients: %w", err)
	}
	var failures []string
	seen := map[string]bool{}
	for _, g := range grades {
		expect, exists := expectations[g.ProductID]
		if !exists || seen[g.ProductID] {
			return nil, fmt.Errorf("unexpected or duplicate graded product id %q", g.ProductID)
		}
		if g.Grade == nil {
			return nil, fmt.Errorf("missing grade for product id %q", g.ProductID)
		}
		seen[g.ProductID] = true
		score := g.Grade.Score
		if score > expect.Max {
			failures = append(failures, fmt.Sprintf("grade=%d>%d  desc=%s reason=%s\n",
				score,
				expect.Max,
				g.Description,
				g.Grade.Reason,
			))
			continue
		}

		if score < expect.Min {
			failures = append(failures, fmt.Sprintf("grade=%d<%d desc=%s reason=%s\n",
				score,
				expect.Min,
				g.Description,
				g.Grade.Reason,
			))
			continue
		}
	}
	if len(seen) != len(expectations) {
		return nil, fmt.Errorf("incomplete ingredient grading: received %d of %d grades", len(seen), len(expectations))
	}
	output := "PASS"
	if len(failures) != 0 {
		output = strings.Join(failures, "\n")
	}
	return map[string]interface{}{
		"output":    output,
		"latencyMs": latency.Milliseconds(),
		"metadata": map[string]interface{}{
			"grades":                grades,
			"ingredientCount":       len(ings),
			"passedIngredientCount": len(ings) - len(failures),
		},
	}, nil
}
