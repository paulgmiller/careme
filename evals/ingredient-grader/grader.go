package grader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

func CallApi(_ string, _ map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %s", err)
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache for ingredient grading: %w", err)
	}
	grader := grading.NewManager(cfg, cacheStore, http.DefaultClient)
	return runEval(ctx, grader)
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
		expectations[ing.ProductID] = eval.Expect
	}

	grades, err := grader.GradeIngredients(context.Background(), ings)
	if err != nil {
		return nil, fmt.Errorf("failed to grade ingredients: %w", err)
	}
	var failures []string
	for _, g := range grades {
		expect := expectations[g.ProductID]
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
	if len(failures) == 0 {
		return map[string]interface{}{
			"output": "PASS",
		}, nil
	}

	return map[string]interface{}{
		"output": strings.Join(failures, "\n"),
	}, nil
}
