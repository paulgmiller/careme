package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

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

func main() {
	// Promptfoo exec args:
	// 1: rendered prompt
	// 2: provider options JSON
	// 3: test context JSON
	if len(os.Args) < 4 {
		log.Fatalf("expected promptfoo arguments")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache for ingredient grading: %s", err)
	}
	grader := grading.NewManager(cfg, cacheStore, http.DefaultClient)

	var pf promptfooContext
	if err := json.Unmarshal([]byte(os.Args[3]), &pf); err != nil {
		log.Fatalf("parse promptfoo context: %v", err)
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

	for _, g := range grades {
		expect := expectations[g.ProductID]
		score := g.Grade.Score
		if score > expect.Max {
			fmt.Printf("FAIL grade=%d>%d reason=%s",
				score,
				expect.Max,
			)
			continue
		}

		if score < expect.Min {
			fmt.Printf("FAIL grade=%d<%d reason=%s",
				score,
				expect.Max,
			)
			continue
		}

		fmt.Printf(
			"PASS grade=%.d expected=%d..%d",
			score,
			expect.Min,
			expect.Max,
		)

	}
}
