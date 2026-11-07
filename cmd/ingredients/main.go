package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/providerregistry"
	"careme/internal/providers/kroger"
	"careme/internal/recipes"

	"github.com/samber/lo"
)

func main() {
	var location string
	var verbose bool
	var ingredient string
	var limit int
	var studyDirectory, candidateFile string
	var requestBudget int
	flag.IntVar(&requestBudget, "staples-request-budget", 39, "Maximum measured product API requests for exact staples optimization")
	flag.StringVar(&studyDirectory, "optimize-staples", "", "Run a graded Kroger query study; save reusable query snapshots and report in this directory")
	flag.StringVar(&candidateFile, "staples-candidates", "", "Optional JSON array of experimental staples searches")
	var searches []string
	var searchBrands string
	flag.StringVar(&searchBrands, "kroger-brands", "", "Pipe-separated API brand filter for diagnostic searches; omitted searches all brands")
	flag.Func("kroger-search", "Inspect a raw Kroger search and compare with staples; repeat for multiple terms (no grading)", func(term string) error {
		term = strings.TrimSpace(term)
		if term == "" {
			return fmt.Errorf("search term must not be blank")
		}
		searches = append(searches, term)
		return nil
	})
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.BoolVar(&verbose, "verbose", false, "dump all ingredients and grades")
	flag.StringVar(&ingredient, "ingredient", "", "Find nearest ingredients to this description")
	flag.IntVar(&limit, "limit", 20, "Number of nearest ingredients to return")
	flag.Parse()
	if strings.TrimSpace(location) == "" {
		log.Fatal("-location is required")
	}
	if limit < 1 {
		log.Fatal("-limit must be positive")
	}
	ctx := context.Background()

	if studyDirectory != "" {
		if err := optimizeStaples(ctx, location, studyOptions{directory: studyDirectory, candidateFile: candidateFile, requestBudget: requestBudget}); err != nil {
			log.Fatal(err)
		}
		return
	}

	if len(searches) > 0 {
		if err := inspectKroger(ctx, location, searches, searchBrands); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	if ingredient != "" && (!cfg.IngredientGrading.Enable || strings.TrimSpace(cfg.AI.APIKey) == "") {
		log.Fatal("nearest ingredient lookup requires ingredient grading enabled and an OpenAI API key")
	}
	backends, err := providerregistry.NewFactory(cfg).NewStaplesBackends()
	if err != nil {
		log.Fatalf("failed to create staples backends: %s", err)
	}
	sp := recipes.NewStaplesProvider(backends)

	ings, err := sp.FetchStaples(ctx, location)
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	catMap := make(map[string]int)

	log.Printf("Grading %d ingredients", len(ings))
	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache for ingredient grading: %s", err)
	}
	grader := ingredientgrading.NewManager(cfg, cacheStore, http.DefaultClient)
	// combe back to embeddigns latter.
	// grader := ingredientgrading.NewEnrichingGrader(cfg, cacheStore, http.DefaultClient)
	graded, err := grader.GradeIngredients(ctx, ings)
	if err != nil {
		log.Fatalf("failed to grade ingredients: %s", err)
	}
	/*if ingredient != "" {
		embedder := ai.NewIngredientEmbedder(cfg.AI.APIKey, http.DefaultClient)
		vectors, err := embedder.EmbedIngredients(ctx, []string{ingredient})
		if err != nil {
			log.Fatal(err)
		}
		neighbors, err := ai.NearestIngredients(vectors[0], graded, limit)
		if err != nil {
			log.Fatal(err)
		}
		for _, n := range neighbors {
			fmt.Printf("%f %s\n", n.Similarity, n.Ingredient.Description)
		}

		return
	}*/
	slices.SortFunc(graded, func(a, b ai.InputIngredient) int {
		if a.Grade.Score != b.Grade.Score {
			return b.Grade.Score - a.Grade.Score
		}
		return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
	})
	for _, result := range graded {
		for _, cat := range result.Categories {
			catMap[cat] += 1
		}
		if verbose {
			fmt.Printf(
				"%2d/10: %s - %s: regular: %s sale: %s: %s\n",
				result.Grade.Score,
				result.Brand,
				result.Description,
				priceString(result.PriceRegular),
				priceString(result.PriceSale),
				result.Grade.Reason,
			)
		}
	}
	for cat, count := range catMap {
		fmt.Printf("Category: %s, Count: %d\n", cat, count)
	}

	counts := lo.Reduce(graded, func(counts map[int]int, ingredient ai.InputIngredient, _ int) map[int]int {
		counts[ingredient.Grade.Score] += 1
		return counts
	}, make(map[int]int))
	fmt.Println("Grade distribution:")
	for score := range 10 {
		fmt.Printf("Score %2d: %d ingredients\n", score, counts[score])
	}
	sumGrades := lo.SumBy(graded, func(ing ai.InputIngredient) int { return ing.Grade.Score })
	fmt.Printf("Total count %d and score %d\n", len(graded), sumGrades)
}

func priceString(price *float32) string {
	if price == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *price)
}

func inspectKroger(ctx context.Context, location string, terms []string, searchBrands string) error {
	cfg, err := config.LoadOnlyProviders()
	if err != nil {
		return fmt.Errorf("load provider configuration: %w", err)
	}
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger inspection requires KROGER_CLIENT_ID and KROGER_CLIENT_SECRET")
	}
	provider, err := kroger.NewStaplesProvider(cfg, http.DefaultClient)
	if err != nil {
		return err
	}
	if !provider.IsID(location) {
		return fmt.Errorf("kroger inspection requires a numeric Kroger location ID")
	}
	report, err := provider.InspectStaples(ctx, location, terms, splitBrands(searchBrands))
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func splitBrands(value string) []string {
	var brands []string
	for brand := range strings.SplitSeq(value, "|") {
		if brand = strings.TrimSpace(brand); brand != "" {
			brands = append(brands, brand)
		}
	}
	return brands
}
