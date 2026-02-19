package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/recipes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
)

var defaultProduce = []string{
	"carrots",
	"broccoli",
	"kale",
	"brussels sprouts",
	"bananas",
	"apples",
	"onions",
	"potatoes",
}

func main() {
	var locationID string
	var produceCSV string

	flag.StringVar(&locationID, "location", "", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "", "Kroger location ID to validate (short)")
	flag.StringVar(&produceCSV, "produce", strings.Join(defaultProduce, ","), "Comma-separated produce list to check")
	flag.Parse()

	if strings.TrimSpace(locationID) == "" {
		log.Fatalf("missing required -location flag")
	}

	produce := parseProduceList(produceCSV)
	if len(produce) == 0 {
		log.Fatalf("no produce terms provided")
	}

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	generator, err := recipes.NewGenerator(cfg, cacheStore)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %v", err)
	}
	g, ok := generator.(*recipes.Generator)
	if !ok {
		log.Fatalf("failed to cast generator to *recipes.Generator")
	}

	missing, err := checkProduceAvailability(ctx, g, locationID, produce)
	if err != nil {
		log.Fatalf("availability check failed: %v", err)
	}

	if len(missing) > 0 {
		fmt.Printf("missing produce for location %s: %s\n", locationID, strings.Join(missing, ", "))
		os.Exit(1)
	}

	fmt.Printf("all produce available for location %s: %s\n", locationID, strings.Join(produce, ", "))
}

func parseProduceList(csv string) []string {
	parts := strings.Split(csv, ",")
	produce := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		term := normalizeTerm(part)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		produce = append(produce, term)
	}
	return produce
}

func checkProduceAvailability(ctx context.Context, g *recipes.Generator, locationID string, produce []string) ([]string, error) {
	filter := recipes.Filter("produce vegetable", nil, false)
	ingredients, err := g.GetIngredients(ctx, locationID, filter, 0)
	if err != nil {
		return nil, err
	}

	missing := make([]string, 0)
	for _, term := range produce {
		available, sample := hasProduce(ingredients, term)
		if available {
			fmt.Printf("✅ %s -> found: %s\n", term, sample)
			continue
		}
		fmt.Printf("❌ %s -> no matching products found\n", term)
		missing = append(missing, term)
	}

	slices.Sort(missing)
	return missing, nil
}

func hasProduce(ingredients []kroger.Ingredient, term string) (bool, string) {
	needle := normalizeTerm(term)
	for _, ingredient := range ingredients {
		if ingredient.Description == nil {
			continue
		}
		description := strings.TrimSpace(*ingredient.Description)
		if description == "" {
			continue
		}
		haystack := normalizeTerm(description)
		if strings.Contains(haystack, needle) {
			return true, description
		}
	}

	return false, ""
}

func normalizeTerm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "brussel sprouts", "brussels sprouts")
	return s
}
