package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"careme/internal/config"
	"careme/internal/recipes"
)

func main() {
	var location string
	var help bool

	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., Seattle, WA)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.BoolVar(&help, "help", false, "Show help message")
	flag.BoolVar(&help, "h", false, "Show help message")
	flag.Parse()

	if help {
		showHelp()
		return
	}

	if location == "" {
		fmt.Println("Error: Location is required")
		showHelp()
		os.Exit(1)
	}

	if err := run(location); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(location string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	generator, err := recipes.NewGenerator(cfg)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}
	generator.GetIngredients(location)

	
	//fmt.Printf("🍽️  Generating 4 weekly recipes for location: %s\n", location)
	//fmt.Println("🏷️  Checking current sales at local QFC/Fred Meyer...")
	//fmt.Println("📚 Avoiding recipes from the past 2 weeks...")
	//fmt.Println()

	generatedRecipes, err := generator.GenerateWeeklyRecipes(location)
	if err != nil {
		return fmt.Errorf("failed to generate recipes: %w", err)
	}

	output := formatter.FormatRecipes(generatedRecipes)
	fmt.Print(output)
	*/

	return nil
}

func showHelp() {
	fmt.Println("Careme - Weekly Recipe Generator")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  careme -location <location>")
	fmt.Println("  careme -l <location>")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -location, -l   Location for recipe sourcing (required)")
	fmt.Println("  -help, -h       Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  careme -location \"Seattle, WA\"")
	fmt.Println("  careme -l \"Portland, OR\"")
}
