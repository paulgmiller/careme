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

	"github.com/samber/lo"
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

	ings = lo.UniqBy(ings, func(i kroger.Ingredient) string {
		return toString(i.ProductId)
	})

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
			if a.Grade.Score != b.Grade.Score {
				return b.Grade.Score - a.Grade.Score
			}
			return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
		})
		for _, result := range graded {
			for _, cat := range result.Categories {
				catMap[cat] += 1
			}

			fmt.Printf("%2d/10: %s - %s: size: %s: %s\n", result.Grade.Score, result.Brand, result.Description, result.Size, result.Grade.Reason)
		}
		for cat, count := range catMap {
			fmt.Printf("Category: %s, Count: %d\n", cat, count)
		}

		counts := lo.Reduce(graded, func(counts map[int]int, ingredient ai.InputIngredient, _ int) map[int]int {
			counts[ingredient.Grade.Score] += 1
			return counts
		}, make(map[int]int))
		fmt.Println("Grade distribution:")
		for score := 0; score <= 10; score++ {
			fmt.Printf("Score %2d: %d ingredients\n", score, counts[score])
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
