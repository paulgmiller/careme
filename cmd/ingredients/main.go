package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/recipes"
	"context"
	"flag"
	"fmt"
	"log"
)

func main() {
	var ingredient string
	var location string
	flag.StringVar(&ingredient, "ingredient", "", "Ingredient to filter recipes")
	flag.StringVar(&ingredient, "i", "", "Ingredient to filter recipes")
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.Parse()
	ctx := context.Background()
	cache, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %s", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %s", err)
	}

	g, ok := generator.(*recipes.Generator)
	if !ok {
		log.Fatalf("failed to cast generator to *recipes.Generator")
	}

	f := recipes.Filter(ingredient, []string{"*"}, false /*frozen*/)
	ings, err := g.GetIngredients(ctx, location, f, 0)
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	for _, i := range ings {
		fmt.Println(*i.Description)
	}

}
