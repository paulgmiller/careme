package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"careme/internal/config"
	"careme/internal/ingredientcoverage"
	"careme/internal/recipes"
)

func main() {
	var locationID string
	var datasetName string
	var termsCSV string

	// local to bellevue Fred Meyer 70100023, factoria 70500822
	flag.StringVar(&locationID, "location", "70500874", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "70500874", "Kroger location ID to validate (short)")
	flag.StringVar(&datasetName, "dataset", ingredientcoverage.DefaultDatasetName(), "Dataset to score: produce, meat, seafood, or all")
	flag.StringVar(&termsCSV, "produce", "", "Comma-separated custom term list to check (legacy flag name)")
	flag.StringVar(&termsCSV, "terms", "", "Comma-separated custom term list to check")
	flag.Parse()

	if strings.TrimSpace(locationID) == "" {
		log.Fatalf("missing required -location flag")
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

	ingredients, err := staples.FetchStaples(ctx, locationID)
	if err != nil {
		log.Fatalf("availability check failed: %v", err)
	}

	datasets, err := selectedDatasets(datasetName, termsCSV)
	if err != nil {
		log.Fatalf("invalid dataset selection: %v", err)
	}

	fmt.Printf("Location %s\n", locationID)
	for _, dataset := range datasets {
		report := ingredientcoverage.Analyze(dataset, ingredients, ingredientcoverage.BaselineMatcher())
		printReport(report)
		fmt.Println()
	}
}

func selectedDatasets(datasetName string, termsCSV string) ([]ingredientcoverage.Dataset, error) {
	customTerms := ingredientcoverage.ParseTermsCSV(termsCSV)
	if len(customTerms) > 0 {
		return []ingredientcoverage.Dataset{{
			Name:  "custom",
			Label: "Custom",
			Terms: customTerms,
		}}, nil
	}
	return ingredientcoverage.DatasetsForSelection(datasetName)
}

func printReport(report ingredientcoverage.Report) {
	fmt.Printf("%s score %.6f: %d/%d with %d ingredients and %d matches\n",
		report.DatasetLabel,
		report.Score,
		report.MatchedTerms,
		report.TotalTerms,
		report.TotalIngredients,
		report.TotalMatches,
	)
	if len(report.MissingTerms) > 0 {
		fmt.Printf("missing %s: %s\n", strings.ToLower(report.DatasetLabel), strings.Join(report.MissingTerms, ", "))
	}
}
