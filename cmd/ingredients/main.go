package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/recipes"

	"github.com/samber/lo"
)

func main() {
	var location string
	var verbose bool
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.BoolVar(&verbose, "verbose", false, "dump all ingredients and grades")
	flag.Parse()
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatal(err)
	}
	sp, err := recipes.NewStaplesProvider(cfg)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %s", err)
	}

	ings, err := sp.FetchStaples(ctx, location)
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	catMap := make(map[string]int)

	log.Printf("Grading %d ingredients", len(ings))
	grader := ingredientgrading.NewManager(cfg, cacheStore, http.DefaultClient)
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
		if verbose {
			fmt.Printf(
				"%2d/10: %s - %s: regular: %s sale: %s: %s\n",
				result.Grade.Score,
				result.Brand,
				result.Description,
				priceString(result.PriceRegular),
				priceString(result.PriceSale),
				result.Grade.Reason,
			)
		}
	}
	for cat, count := range catMap {
		fmt.Printf("Category: %s, Count: %d\n", cat, count)
	}

	counts := lo.Reduce(graded, func(counts map[int]int, ingredient ai.InputIngredient, _ int) map[int]int {
		counts[ingredient.Grade.Score] += 1
		return counts
	}, make(map[int]int))
	fmt.Println("Grade distribution:")
	for score := range 10 {
		fmt.Printf("Score %2d: %d ingredients\n", score, counts[score])
	}
	sumGrades := lo.SumBy(graded, func(ing ai.InputIngredient) int { return ing.Grade.Score })
	fmt.Printf("Total count %d and score %d\n", len(graded), sumGrades)
}

func priceString(price *float32) string {
	if price == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *price)
}
