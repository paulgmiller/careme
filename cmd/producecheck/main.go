package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode"

	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/recipes"

	"github.com/samber/lo"
	"golang.org/x/text/unicode/norm"
)

type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
}

func main() {
	var locationID string
	var produceCSV string

	// local to bellevue Fred Meyer 70100023, factoria 70500822
	flag.StringVar(&locationID, "location", "70500874", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "70500874", "Kroger location ID to validate (short)")
	flag.StringVar(&produceCSV, "produce", strings.Join(all, ","), "Comma-separated produce list to check")
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

type produceFilterStats struct {
	FilterTerm          string
	IngredientMatches   int
	ProduceTermsMatched int
	ProduceMatches      int
	UniqueOnlyMatches   int
	matchedDescriptions []string
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

	ingredients = lo.UniqBy(ingredients, func(i kroger.Ingredient) string {
		return toString(i.ProductId)
	})

	// TODO have staples return subset of ingredient that can say the search term it go a match on
	// then we can give info on whats coming from what queries. Could use categories for wholefoods.
	// annotateUniqueOnlyMatches(stats)
	printProduceFilterSummary(stats, len(produce))

	return evaluateProduceAvailability(produce, ingredients), len(ingredients), nil
}

func toString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func evaluateProduceAvailability(produce []string, ingredients []kroger.Ingredient) []string {
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

func summarizeFilterMatches(produce []string, ingredients []kroger.Ingredient) (int, int) {
	matchedTerms, matchedProducts, _ := summarizeFilterMatchesDetailed(produce, ingredients)
	return matchedTerms, matchedProducts
}

func summarizeFilterMatchesDetailed(produce []string, ingredients []kroger.Ingredient) (int, int, []string) {
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

func hasProduce(ingredients []kroger.Ingredient, term string) []string {
	needleTokens := tokens(normalizeTerm(term))
	if len(needleTokens) == 0 {
		return nil
	}
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
		haystackTokens := tokens(normalizeTerm(description))
		if !containsAllTokens(haystackTokens, needleTokens) {
			continue
		}
		if _, ok := seen[description]; ok {
			continue
		}
		seen[description] = struct{}{}
		matches = append(matches, description)
	}

	slices.Sort(matches)
	return matches
}

func normalizeTerm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = removeParenthetical(s)
	s = stripDiacritics(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	fields := strings.Fields(b.String())
	if len(fields) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		token := normalizeToken(field)
		if token == "" {
			continue
		}
		normalized = append(normalized, token)
	}
	return strings.Join(normalized, " ")
}

func tokens(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func containsAllTokens(haystack []string, needle []string) bool {
	if len(needle) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(haystack))
	for _, token := range haystack {
		set[token] = struct{}{}
	}
	for _, token := range needle {
		if _, ok := set[token]; !ok {
			return false
		}
	}
	return true
}

func removeParenthetical(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func stripDiacritics(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

func normalizeToken(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		s = s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "oes") && len(s) > 3:
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "ches") || strings.HasSuffix(s, "shes") || strings.HasSuffix(s, "xes") || strings.HasSuffix(s, "zes") || strings.HasSuffix(s, "ses"):
		if len(s) > 4 {
			s = s[:len(s)-2]
		}
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 2:
		s = s[:len(s)-1]
	}

	switch s {
	case "kiwifruit":
		s = "kiwi"
	case "asparagus":
		return s
	case "portobello":
		s = "portabella"
	case "chile":
		s = "chili"
	}

	switch s {
	case "brussel":
		return "brussels"
	}
	return s
}
