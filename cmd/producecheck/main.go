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
	var compareStemmer bool

	// local to bellevue Fred Meyer 70100023, factoria 70500822
	flag.StringVar(&locationID, "location", "70500874", "Kroger location ID to validate")
	flag.StringVar(&locationID, "l", "70500874", "Kroger location ID to validate (short)")
	flag.StringVar(&datasetName, "dataset", ingredientcoverage.DefaultDatasetName(), "Dataset to score: produce, meat, seafood, or all")
	flag.StringVar(&termsCSV, "produce", "", "Comma-separated custom term list to check (legacy flag name)")
	flag.StringVar(&termsCSV, "terms", "", "Comma-separated custom term list to check")
	flag.BoolVar(&compareStemmer, "compare-stemmer", false, "Also compare the current matcher against the stemmed matcher")
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
		if compareStemmer {
			printComparison(ingredientcoverage.Compare(dataset, ingredients))
		}
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

func printComparison(comparison ingredientcoverage.Comparison) {
	fmt.Printf("Stemmer compare for %s: baseline %d/%d vs stemmed %d/%d\n",
		comparison.DatasetLabel,
		comparison.Baseline.MatchedTerms,
		comparison.Baseline.TotalTerms,
		comparison.Stemmed.MatchedTerms,
		comparison.Stemmed.TotalTerms,
	)
	if len(comparison.AddedTerms) > 0 {
		fmt.Printf("stemmer gained terms: %s\n", strings.Join(comparison.AddedTerms, ", "))
	}
	if len(comparison.RemovedTerms) > 0 {
		fmt.Printf("stemmer lost terms: %s\n", strings.Join(comparison.RemovedTerms, ", "))
	}
}
