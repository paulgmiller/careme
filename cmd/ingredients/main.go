package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"careme/internal/config"
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
	for _, i := range ings {
		for _, cat := range *i.Categories {
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
