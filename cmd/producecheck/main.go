package main

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
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

	client, err := kroger.FromConfig(cfg)
	if err != nil {
		log.Fatalf("failed to create kroger client: %v", err)
	}

	missing, err := checkProduceAvailability(ctx, client, locationID, produce)
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

func checkProduceAvailability(ctx context.Context, client *kroger.ClientWithResponses, locationID string, produce []string) ([]string, error) {
	missing := make([]string, 0)
	for _, term := range produce {
		available, sample, err := hasProduce(ctx, client, locationID, term)
		if err != nil {
			return nil, err
		}
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

func hasProduce(ctx context.Context, client *kroger.ClientWithResponses, locationID, term string) (bool, string, error) {
	limit := "10"
	response, err := client.ProductSearchWithResponse(ctx, &kroger.ProductSearchParams{
		FilterLocationId: &locationID,
		FilterTerm:       &term,
		FilterLimit:      &limit,
	})
	if err != nil {
		return false, "", fmt.Errorf("search %q: %w", term, err)
	}
	if response.StatusCode() != http.StatusOK {
		return false, "", fmt.Errorf("search %q: unexpected status %d", term, response.StatusCode())
	}
	if response.JSON200 == nil || response.JSON200.Data == nil {
		return false, "", errors.New("empty search response")
	}

	needle := normalizeTerm(term)
	for _, product := range *response.JSON200.Data {
		if product.Description == nil {
			continue
		}
		description := strings.TrimSpace(*product.Description)
		if description == "" {
			continue
		}
		haystack := normalizeTerm(description)
		if strings.Contains(haystack, needle) {
			return true, description, nil
		}
	}

	return false, "", nil
}

func normalizeTerm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "brussel sprouts", "brussels sprouts")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
