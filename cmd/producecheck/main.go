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
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var fruit = []string{
	"bananas",
	"apples",
	"pears",
	"oranges",
	"cherries",
	"grapes",
	"strawberries",
	"blueberries",
	"raspberries",
	"blackberries",
	"watermelon",
	"cantaloupe",
	"honeydew melon",
	"kiwi",
	"pineapple",
	"mangoes",
}

var tubers = []string{
	"onions",
	"potatoes",
}

var vegetables = []string{
	// Leafy greens & lettuces
	"Romaine lettuce",
	"Green leaf lettuce",
	"Red leaf lettuce",
	"Iceberg lettuce",
	"Butterhead lettuce",
	"Little gem lettuce",
	"Spring mix",
	"Baby spring mix",
	"Arugula",
	"Baby arugula",
	"Spinach",
	"Baby spinach",
	"Kale",
	"Curly kale",
	"Lacinato kale",
	"Rainbow chard",
	"Bok choy",
	"Baby bok choy",
	"Napa cabbage",
	"Green cabbage",
	"Red cabbage",
	"Radicchio",

	// Brassicas
	"Broccoli",
	"Broccolini",
	"Cauliflower",
	"Brussels sprouts",

	// Roots & tubers
	"Carrots",
	"Baby carrots",
	"Rainbow carrots",
	"Beets",
	"Golden beets",
	"Turnips",
	"Rutabaga",
	"Parsnips",
	"Daikon radish",
	"Radishes",
	"Horseradish root",
	"Celery root (celeriac)",
	"Jicama",
	"Yuca (cassava)",

	// Alliums
	"Green onions (scallions)",
	"Leeks",
	"Garlic",

	// Stalks & stems
	"Celery",
	"Asparagus",
	"Lemongrass",

	// Fruiting vegetables
	"Green bell peppers",
	"Red bell peppers",
	"Yellow bell peppers",
	"Orange bell peppers",
	"Mini sweet peppers",
	"Poblano peppers",
	"Jalapeño peppers",
	"Serrano peppers",
	"Anaheim peppers",
	"Habanero peppers",
	"Red chili peppers",
	"Green chili peppers",
	"Tomatillos",
	"Zucchini",
	"Yellow squash",
	"Cucumber",
	"Mini cucumbers",
	"Seedless cucumbers",
	"Eggplant",
	"Green beans",
	"Sweet corn",

	// Mushrooms
	"White mushrooms",
	"Baby bella (cremini) mushrooms",
	"Portobello mushrooms",
	"Shiitake mushrooms",
	"Oyster mushrooms",
	"King trumpet mushrooms",
	"Sliced mushroom blend",

	// Herbs
	"Parsley",
	"Italian parsley",
	"Cilantro",
	"Basil",
	"Thyme",
	"Sage",
	"Rosemary",
	"Tarragon",
	"Dill",
	"Chives",

	// Sprouts & microgreens
	"Alfalfa sprouts",
	"Broccoli sprouts",
	"Mixed sprouts",

	// Other
	"Aloe vera leaf",
	"Bean sprouts",
}

var all = append(append(fruit, tubers...), vegetables...)

func main() {
	var locationID string
	var produceCSV string
	var singleFilterTerm string

	//local to bellevue Fred Meyer 70100023, factoria 70500822
	flag.StringVar(&locationID, "location", "70500874", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "70500874", "Kroger location ID to validate (short)")
	flag.StringVar(&produceCSV, "produce", strings.Join(all, ","), "Comma-separated produce list to check")
	flag.StringVar(&singleFilterTerm, "single-filter", "", "If set, fetch ingredients using only this Kroger filter term (e.g. 'produce')")
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

	missing, results, err := checkProduceAvailability(ctx, g, locationID, produce, singleFilterTerm)
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

type produceMatchStats struct {
	Term     string
	Matches  []string
	Shortest string
	Longest  string
}

func checkProduceAvailability(ctx context.Context, g *recipes.Generator, locationID string, produce []string, singleFilterTerm string) ([]string, int, error) {
	if strings.TrimSpace(singleFilterTerm) != "" {
		filter := recipes.Filter(singleFilterTerm, []string{"*"}, false /*frozen*/)
		ingredients, err := g.GetIngredients(ctx, locationID, filter, 0)
		if err != nil {
			return nil, 0, err
		}
		return evaluateProduceAvailability(produce, ingredients), len(ingredients), nil
	}

	ingredients := g.GetIngredientsForFilters(ctx, locationID, recipes.Produce()...)

	return evaluateProduceAvailability(produce, ingredients), len(ingredients), nil
}

func evaluateProduceAvailability(produce []string, ingredients []kroger.Ingredient) []string {
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
			if len(stats.Matches) == 1 {
				fmt.Printf("- %s (%d match): %s\n", stats.Term, len(stats.Matches), stats.Matches[0])
				continue
			}

			fmt.Printf("- %s (%d matches)\n", stats.Term, len(stats.Matches))
			fmt.Printf("  shortest: %s\n", stats.Shortest)
			fmt.Printf("  longest: %s\n", stats.Longest)
			//fmt.Println("  descriptions:")
			//for _, description := range stats.Matches {
			//	fmt.Printf("  - %s\n", description)
			//}
		}
	}

	slices.Sort(missing)
	return missing
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
			if token == "lettuce" {
				continue
			}
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
	case "brussel":
		return "brussels"
	}
	return s
}
