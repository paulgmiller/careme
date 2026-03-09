package main

import (
	"careme/internal/config"
	"careme/internal/recipes"
	"context"
	"flag"
	"fmt"
	"log"
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

	ings, err := sp.GetIngredients(ctx, location, searchTerm, 0)
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	for _, i := range ings {
		fmt.Printf("%s - %s:(%s)\n", toString(i.Brand), toString(i.Description), toFloat(i.PriceRegular))
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
