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
	"carrot",
	"broccoli",
	"celery",
	"kale",
	"brussel sprouts",
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

type produceMatchStats struct {
	Term     string
	Matches  []string
	Shortest string
	Longest  string
}

func checkProduceAvailability(ctx context.Context, g *recipes.Generator, locationID string, produce []string) ([]string, error) {
	filter := recipes.Filter("produce vegatable", nil, false)
	ingredients, err := g.GetIngredients(ctx, locationID, filter, 0)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Looking through %d produce vegatable results\n", len(ingredients))

	missing := make([]string, 0)
	foundStats := make([]produceMatchStats, 0, len(produce))
	for _, term := range produce {
		matches := hasProduce(ingredients, term)
		if len(matches) > 0 {
			shortest, longest := shortestAndLongest(matches)
			foundStats = append(foundStats, produceMatchStats{
				Term:     term,
				Matches:  matches,
				Shortest: shortest,
				Longest:  longest,
			})
			fmt.Printf("✅ %s -> %d matches\n", term, len(matches))
			continue
		}
		fmt.Printf("❌ %s -> no matching products found\n", term)
		missing = append(missing, term)
	}

	if len(foundStats) > 0 {
		fmt.Println()
		fmt.Println("match summary:")
		for _, stats := range foundStats {
			fmt.Printf("- %s (%d matches)\n", stats.Term, len(stats.Matches))
			fmt.Printf("  shortest: %s\n", stats.Shortest)
			fmt.Printf("  longest: %s\n", stats.Longest)
			fmt.Println("  descriptions:")
			for _, description := range stats.Matches {
				fmt.Printf("  - %s\n", description)
			}
		}
	}

	slices.Sort(missing)
	return missing, nil
}

func hasProduce(ingredients []kroger.Ingredient, term string) []string {
	needle := normalizeTerm(term)
	seen := make(map[string]struct{})
	matches := make([]string, 0)
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
			if _, ok := seen[description]; ok {
				continue
			}
			seen[description] = struct{}{}
			matches = append(matches, description)
		}
	}

	slices.Sort(matches)
	return matches
}

func shortestAndLongest(matches []string) (string, string) {
	shortest := matches[0]
	longest := matches[0]
	for _, match := range matches[1:] {
		if len(match) < len(shortest) {
			shortest = match
		}
		if len(match) > len(longest) {
			longest = match
		}
	}
	return shortest, longest
}

func normalizeTerm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "brussel sprouts", "brussels sprouts")
	return s
}
