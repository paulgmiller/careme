package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

/*func main() {
	// Promptfoo exec args:
	// 1: rendered prompt
	// 2: provider options JSON
	// 3: test context JSON
	if len(os.Args) < 4 {
		log.Fatalf("expected promptfoo arguments")
	}

	var ctxmap map[string]any

	lo.Must0(json.Unmarshal([]byte(os.Args[3]), &ctxmap))
	out := lo.Must(CallApi("", nil, ctxmap))
	fmt.Println(lo.Must(json.Marshal(out)))

}*/

func CallApi(_ string, _ map[string]interface{}, ctx map[string]interface{}) (map[string]interface{}, error) {

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %s", err)
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache for ingredient grading: %s", err)
	}
	grader := grading.NewManager(cfg, cacheStore, http.DefaultClient)

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
		log.Fatal(err)
	}
	var failures []string
	for _, g := range grades {
		expect := expectations[g.ProductID]
		score := g.Grade.Score
		if score > expect.Max {
			failures = append(failures, fmt.Sprintf("grade=%d>%d reason=%s\n",
				score,
				expect.Max,
				g.Grade.Reason,
			))
			continue
		}

		if score < expect.Min {
			failures = append(failures, fmt.Sprintf("grade=%d<%d reason=%s\n",
				score,
				expect.Max,
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
