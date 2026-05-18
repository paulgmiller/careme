package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/produce"
	"careme/internal/recipes"
)

func main() {
	var locationID string
	var produceCSV string

	// local to bellevue Fred Meyer 70100023, factoria 70500822
	flag.StringVar(&locationID, "location", "70500874", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "70500874", "Kroger location ID to validate (short)")
	flag.StringVar(&produceCSV, "produce", strings.Join(produce.DefaultTerms(), ","), "Comma-separated produce list to check")
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

	staples, err := recipes.NewStaplesProvider(cfg)
	if err != nil {
		log.Fatalf("failed to create staples provider: %v", err)
	}

	missing, results, err := checkProduceAvailability(ctx, staples, locationID, produce)
	if err != nil {
		log.Fatalf("availability check failed: %v", err)
	}

	if len(missing) > 0 {
		fmt.Printf("missing produce for location %s: %s\n", locationID, strings.Join(missing, ", "))
	}

	fmt.Printf("Produce score  %f: %d/%d with %d ingredients\n", float64(len(produce)-len(missing))/float64(len(produce)), len(produce)-len(missing), len(produce), results)
}

func parseProduceList(csv string) []string {
	parts := strings.Split(csv, ",")
	produceTerms := make([]string, 0, len(parts))
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
		produceTerms = append(produceTerms, term)
	}
	return produceTerms
}

type produceFilterStats struct {
	FilterTerm          string
	IngredientMatches   int
	ProduceTermsMatched int
	ProduceMatches      int
	UniqueOnlyMatches   int
	matchedDescriptions []string
}

type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error)
}

func checkProduceAvailability(ctx context.Context, client staplesProvider, locationID string, produce []string) ([]string, int, error) {
	// todo check total number of queries.

	ingredients, err := client.FetchStaples(ctx, locationID)
	if err != nil {
		log.Fatalf("warning: failed to fetch staples ingredients for location %s: %v", locationID, err)
	}
	matchedTerms, matchedProducts, matchedDescriptions := summarizeFilterMatchesDetailed(produce, ingredients)
	stats := produceFilterStats{
		FilterTerm:          "*",
		IngredientMatches:   len(ingredients),
		ProduceTermsMatched: matchedTerms,
		ProduceMatches:      matchedProducts,
		matchedDescriptions: matchedDescriptions,
	}

	// TODO have staples return subset of ingredient that can say the search term it go a match on
	// then we can give info on whats coming from what queries. Could use categories for wholefoods.
	// annotateUniqueOnlyMatches(stats)
	printProduceFilterSummary(stats, len(produce))

	return evaluateProduceAvailability(produce, ingredients), len(ingredients), nil
}

func evaluateProduceAvailability(produce []string, ingredients []ai.InputIngredient) []string {
	missing := make([]string, 0)
	for _, term := range produce {
		matches := hasProduce(ingredients, term)
		if len(matches) > 0 {
			fmt.Printf("✅ %s -> %d matches\n", term, len(matches))
			continue
		}
		fmt.Printf("❌ %s -> no matching products found\n", term)
		missing = append(missing, term)
	}

	slices.Sort(missing)
	return missing
}

func summarizeFilterMatches(produce []string, ingredients []ai.InputIngredient) (int, int) {
	matchedTerms, matchedProducts, _ := summarizeFilterMatchesDetailed(produce, ingredients)
	return matchedTerms, matchedProducts
}

func summarizeFilterMatchesDetailed(produce []string, ingredients []ai.InputIngredient) (int, int, []string) {
	matchedTerms := 0
	matchedProducts := 0
	descriptions := make(map[string]struct{})
	for _, term := range produce {
		matches := hasProduce(ingredients, term)
		if len(matches) == 0 {
			continue
		}
		matchedTerms++
		matchedProducts += len(matches)
		for _, description := range matches {
			descriptions[description] = struct{}{}
		}
	}
	matchedDescriptions := make([]string, 0, len(descriptions))
	for description := range descriptions {
		matchedDescriptions = append(matchedDescriptions, description)
	}
	slices.Sort(matchedDescriptions)
	return matchedTerms, matchedProducts, matchedDescriptions
}

// see what filters return results NOT seen elsewhere then update UniqueOnlyMatches
/*func annotateUniqueOnlyMatches(stats []produceFilterStats) {
	descriptionCount := make(map[string]int)
	for _, stat := range stats {
		for _, description := range stat.matchedDescriptions {
			descriptionCount[description]++
		}
	}

	for i := range stats {
		uniqueOnly := 0
		for _, description := range stats[i].matchedDescriptions {
			if descriptionCount[description] == 1 {
				uniqueOnly++
			}
		}
		stats[i].UniqueOnlyMatches = uniqueOnly
	}
}*/

func printProduceFilterSummary(stat produceFilterStats, totalProduceTerms int) {
	fmt.Printf("- %s -> %d ingredients, %d/%d produce terms, %d matches, %d unique-only products\n",
		stat.FilterTerm,
		stat.IngredientMatches,
		stat.ProduceTermsMatched,
		totalProduceTerms,
		stat.ProduceMatches,
		stat.UniqueOnlyMatches,
	)
	fmt.Println()
}

func hasProduce(ingredients []ai.InputIngredient, term string) []string {
	return produce.MatchDescriptions(ingredients, term)
}

func normalizeTerm(s string) string {
	return produce.NormalizeTerm(s)
}
