package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/recipes"
)

func main() {
	var searchTerm string
	var location string
	flag.StringVar(&searchTerm, "ingredient", "", "Search term for ingredient lookup")
	flag.StringVar(&searchTerm, "i", "", "Search term for ingredient lookup")
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.Parse()
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	sp, err := recipes.NewStaplesProvider(cfg)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %s", err)
	}

	var ings []kroger.Ingredient
	if searchTerm == "" {
		ings, err = sp.FetchStaples(ctx, location)
	} else {
		ings, err = sp.GetIngredients(ctx, location, searchTerm, 0)
	}
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	catMap := make(map[string]int)
	if cfg.IngredientGrading.Enable {
		log.Printf("Grading %d ingredients", len(ings))
		cacheStore, err := cache.MakeCache()
		if err != nil {
			log.Fatalf("failed to create cache for ingredient grading: %s", err)
		}
		grader := ingredientgrading.NewManager(cfg, cacheStore)
		graded, err := grader.GradeIngredients(ctx, ings)
		if err != nil {
			log.Fatalf("failed to grade ingredients: %s", err)
		}
		slices.SortFunc(graded, func(a, b ai.InputIngredient) int {
			ascore := 0
			bscore := 0
			if a.Grade != nil {
				ascore = a.Grade.Score
			}
			if b.Grade != nil {
				bscore = b.Grade.Score
			}
			if ascore != bscore {
				return bscore - ascore
			}
			return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
		})
		for _, result := range graded {
			for _, cat := range result.Categories {
				catMap[cat] += 1
			}
			score := 0
			reason := ""
			if result.Grade != nil {
				score = result.Grade.Score
				reason = result.Grade.Reason
			}
			fmt.Printf("%2d/10 %s: %s - %s:($%s) size: %s categories: %v\n", score, result.ProductID, result.Brand, result.Description, result.PriceRegular, result.Size, result.Categories)
			if strings.TrimSpace(reason) != "" {
				fmt.Printf("    %s\n", strings.TrimSpace(reason))
			}
		}
		for cat, count := range catMap {
			fmt.Printf("Category: %s, Count: %d\n", cat, count)
		}
		return
	}

	for _, i := range ings {
		for _, cat := range categories(i) {
			catMap[cat] += 1
		}
		fmt.Printf("%s: %s - %s:($%s) size: %s categories: %v\n", toString(i.ProductId), toString(i.Brand), toString(i.Description), toFloat(i.PriceRegular), toString(i.Size), i.Categories)
	}
	for cat, count := range catMap {
		fmt.Printf("Category: %s, Count: %d\n", cat, count)
	}
}

func toString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toFloat(f *float32) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *f)
}

func categories(i kroger.Ingredient) []string {
	if i.Categories == nil {
		return nil
	}
	return *i.Categories
}
