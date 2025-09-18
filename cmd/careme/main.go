package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
)

//go:embed html/spinner.html
var spinnerHTML []byte

func main() {
	var location string
	var zipcode string
	var ingredient string
	var serve bool
	var addr string

	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.StringVar(&zipcode, "zipcode", "", "return location ids for a zip code.")
	flag.StringVar(&zipcode, "z", "", "return location ids for a zip code (short form)")
	flag.StringVar(&ingredient, "ingredient", "", "just list ingredients")
	flag.StringVar(&ingredient, "i", "", "just list ingredients (short form)")
	flag.BoolVar(&serve, "serve", false, "Run HTTP server mode")
	flag.StringVar(&addr, "addr", ":8080", "Address to bind in server mode")
	flag.Parse()

	if err := os.MkdirAll("recipes", 0755); err != nil {
		log.Fatalf("failed to create recipes directory: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	if serve {
		if err := runServer(cfg, addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	if zipcode != "" {
		locs, err := locations.GetLocationsByZip(context.TODO(), cfg, zipcode)
		if err != nil {
			log.Fatalf("failed to get locations for zip %s: %v", zipcode, err)
		}
		fmt.Printf("Locations for zip code %s:\n", zipcode)
		for _, loc := range locs {
			fmt.Printf("- %s, %s: %s\n", loc.Name, loc.Address, loc.ID)
		}
		return
	}

	if location == "" {
		fmt.Println("Error: Location is required (or use -serve for web mode)")
		os.Exit(1)
	}

	if err := run(cfg, location, ingredient); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(cfg *config.Config, location string, ingredient string) error {

	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	if ingredient != "" {
		f := recipes.Filter(ingredient, []string{"*"})
		ings, err := generator.GetIngredients(location, f, 0)
		if err != nil {
			return fmt.Errorf("failed to get ingredients: %w", err)
		}
		for _, ing := range ings {
			fmt.Printf("- %s\n", ing)
		}
		return nil
	}

	l, err := locations.GetLocationByID(context.TODO(), cfg, location) // get details but ignore error
	if err != nil {
		return fmt.Errorf("could not get location details: %w", err)
	}

	p := recipes.DefaultParams(l, time.Now())
	generatedRecipes, err := generator.GenerateRecipes(p)
	if err != nil {
		return fmt.Errorf("failed to generate recipes: %w", err)
	}

	//output := formatter.FormatRecipes(generatedRecipes)
	fmt.Println(generatedRecipes)

	return nil
}
