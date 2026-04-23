package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"slices"
	"strings"

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
		results := gradedIngredients(ctx, grader, cliGradeLocationHash(location, searchTerm), ings)
		slices.SortFunc(results, func(a, b gradedIngredient) int {
			if a.score != b.score {
				return b.score - a.score
			}
			return strings.Compare(strings.ToLower(toString(a.ingredient.Description)), strings.ToLower(toString(b.ingredient.Description)))
		})
		for _, result := range results {
			for _, cat := range categories(result.ingredient) {
				catMap[cat] += 1
			}
			fmt.Printf("%2d/10 %s: %s - %s:($%s) size: %s categories: %v\n", result.score, toString(result.ingredient.ProductId), toString(result.ingredient.Brand), toString(result.ingredient.Description), toFloat(result.ingredient.PriceRegular), toString(result.ingredient.Size), result.ingredient.Categories)
			if strings.TrimSpace(result.reason) != "" {
				fmt.Printf("    %s\n", strings.TrimSpace(result.reason))
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

type gradedIngredient struct {
	ingredient kroger.Ingredient
	score      int
	reason     string
}

func gradedIngredients(ctx context.Context, grader ingredientgrading.Service, locationHash string, ingredients []kroger.Ingredient) []gradedIngredient {
	graded := make([]gradedIngredient, 0, len(ingredients))
	grades := grader.GradeIngredients(ctx, locationHash, ingredients)
	for result := range grades {
		entry := gradedIngredient{
			ingredient: result.Ingredient,
		}
		if result.Err == nil {
			entry.score = result.Grade.Score
			entry.reason = result.Grade.Reason
		}
		graded = append(graded, entry)
	}
	return graded
}

func cliGradeLocationHash(location, searchTerm string) string {
	fnv := fnv.New64a()
	_, _ = fnv.Write([]byte(strings.TrimSpace(location)))
	_, _ = fnv.Write([]byte{0})
	_, _ = fnv.Write([]byte(strings.TrimSpace(searchTerm)))
	return "cli-" + base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func categories(i kroger.Ingredient) []string {
	if i.Categories == nil {
		return nil
	}
	return *i.Categories
}
