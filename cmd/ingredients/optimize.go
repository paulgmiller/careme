package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/providers/kroger"
	"careme/internal/recipes/producescore"
)

type studyCatalog struct {
	Search      kroger.StaplesSearch `json:"search"`
	Requests    int                  `json:"requests"`
	Ingredients []ai.InputIngredient `json:"ingredients"`
}

type queryScore struct {
	Search         kroger.StaplesSearch `json:"search"`
	Requests       int                  `json:"requests"`
	Products       int                  `json:"products"`
	UsefulProducts int                  `json:"usefulProducts"`
	GradePoints    int                  `json:"gradePoints"`
}

type searchSetScore struct {
	Searches       []int `json:"searches"`
	Requests       int   `json:"requests"`
	Products       int   `json:"products"`
	UsefulProducts int   `json:"usefulProducts"`
	GradePoints    int   `json:"gradePoints"`
	ProduceScore   int   `json:"produceScore"`
}

type studyReport struct {
	RequestBudget   int              `json:"requestBudget"`
	RequestFrontier []searchSetScore `json:"requestFrontier"`
	Optimal         searchSetScore   `json:"optimalUnderBudget"`
	Location        string           `json:"location"`
	Baseline        searchSetScore   `json:"baseline"`
	Queries         []queryScore     `json:"queries"`
	Frontier        []searchSetScore `json:"greedyFrontier"`
}

type countingTransport struct {
	base     http.RoundTripper
	requests atomic.Int64
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == "/v1/products" {
		t.requests.Add(1)
	}
	return t.base.RoundTrip(req)
}

type studyOptions struct {
	directory     string
	candidateFile string
	requestBudget int
}

func optimizeStaples(ctx context.Context, location string, options studyOptions) error {
	if options.requestBudget < 1 {
		return fmt.Errorf("request budget must be positive")
	}
	directory, candidateFile := options.directory, options.candidateFile
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load study configuration: %w", err)
	}
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("staples optimization requires AI_API_KEY for real ingredient grades")
	}
	cfg.IngredientGrading.Enable = true
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	transport := &countingTransport{base: http.DefaultTransport}
	httpClient := &http.Client{Transport: transport}
	provider, err := kroger.NewStaplesProvider(cfg, httpClient)
	if err != nil {
		return err
	}
	if !provider.IsID(location) {
		return fmt.Errorf("staples optimization requires a Kroger location ID")
	}
	baseline, err := studyBaseline(directory, location)
	if err != nil {
		return err
	}
	candidates := studyCandidates()
	if candidateFile != "" {
		data, err := os.ReadFile(candidateFile)
		if err != nil {
			return err
		}
		candidates = nil
		if err := json.Unmarshal(data, &candidates); err != nil {
			return err
		}
	}
	searches := append(slices.Clone(baseline), candidates...)
	catalogs := make([]studyCatalog, 0, len(searches))
	seenSearches := map[string]bool{}
	for _, search := range searches {
		key, err := json.Marshal(search)
		if err != nil {
			return err
		}
		if seenSearches[string(key)] {
			continue
		}
		seenSearches[string(key)] = true
		hash := sha256.Sum256(append([]byte(location+"/"), key...))
		filename := filepath.Join(directory, fmt.Sprintf("query-%x.json", hash))
		var catalog studyCatalog
		data, err := os.ReadFile(filename)
		switch {
		case err == nil:
			if err := json.Unmarshal(data, &catalog); err != nil {
				return fmt.Errorf("read %s: %w", filename, err)
			}
		case errors.Is(err, os.ErrNotExist):
			before := transport.requests.Load()
			ingredients, err := provider.SearchStaples(ctx, location, search)
			if err != nil {
				return err
			}
			catalog = studyCatalog{Search: search, Requests: int(transport.requests.Load() - before), Ingredients: ingredients}
			if err := writeStudyJSON(filename, catalog); err != nil {
				return err
			}
		default:
			return err
		}
		log.Printf("Study query %d: %s brands=%v api=%t: %d products, %d requests", len(catalogs), search.Term, search.Brands, search.APIBrands, len(catalog.Ingredients), catalog.Requests)
		catalogs = append(catalogs, catalog)
	}
	// Grade each unique product once, sharing the same grade between query sets.
	// The study uses a private disk cache rather than writing production inventory.
	union := map[string]ai.InputIngredient{}
	var inputs []ai.InputIngredient
	for _, catalog := range catalogs {
		for _, ingredient := range catalog.Ingredients {
			if ingredient.ProductID == "" {
				return fmt.Errorf("study query %q returned an empty product ID", catalog.Search.Term)
			}
			if _, ok := union[ingredient.ProductID]; !ok {
				inputs = append(inputs, ingredient)
				union[ingredient.ProductID] = ingredient
			}
		}
	}
	log.Printf("Grading %d unique study products", len(inputs))
	grader := ingredientgrading.NewManager(cfg, cache.NewFileCache(filepath.Join(directory, "grades")), httpClient)
	graded, err := grader.GradeIngredients(ctx, inputs)
	if err != nil {
		return err
	}
	points := map[string]int{}
	for _, ingredient := range graded {
		if ingredient.Grade == nil {
			return fmt.Errorf("missing grade for %s", ingredient.ProductID)
		}
		if ingredient.Grade.Score > producescore.IngredientGradeCutoff {
			points[ingredient.ProductID] = ingredient.Grade.Score
		}
	}
	if len(graded) != len(inputs) {
		return fmt.Errorf("grader returned %d of %d products", len(graded), len(inputs))
	}
	if err := writeStudyJSON(filepath.Join(directory, "graded.json"), graded); err != nil {
		return err
	}
	report := evaluateStudy(location, catalogs, points, len(baseline))
	log.Printf("Finding the highest score within %d measured API requests", options.requestBudget)
	report.RequestBudget = options.requestBudget
	report.RequestFrontier, err = optimalStudyFrontier(ctx, catalogs, points, options.requestBudget)
	if err != nil {
		return err
	}
	report.Optimal = report.RequestFrontier[len(report.RequestFrontier)-1]
	if err := writeStudyJSON(filepath.Join(directory, "report.json"), report); err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func writeStudyJSON(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(data, '\n'), 0o600)
}

func studyCandidates() []kroger.StaplesSearch {
	brands := []string{"Kroger", "Simple Truth", "Foster Farms", "Draper Valley", "PORK", "Harris Teeter", "Sand Bar"}
	searches := []kroger.StaplesSearch{{Term: "Meat & Seafood", Brands: brands, APIBrands: true}}
	for _, term := range []string{"Produce", "Vegetables", "Fruit", "fresh vegetables", "fresh fruit", "Meat & Seafood", "Seafood", "meat", "Rice & Grains", "Beans", "legumes", "nuts", "Dairy & Eggs"} {
		searches = append(searches, kroger.StaplesSearch{Term: term, Brands: []string{"*"}})
	}
	return searches
}

func evaluateStudy(location string, catalogs []studyCatalog, points map[string]int, baselineCount int) studyReport {
	report := studyReport{Location: location}
	for i, catalog := range catalogs {
		score := scoreSearchSet(catalogs, points, []int{i})
		report.Queries = append(report.Queries, queryScore{Search: catalog.Search, Requests: catalog.Requests, Products: score.Products, UsefulProducts: score.UsefulProducts, GradePoints: score.GradePoints})
	}
	baseline := make([]int, baselineCount)
	for i := range baseline {
		baseline[i] = i
	}
	report.Baseline = scoreSearchSet(catalogs, points, baseline)
	// Greedy weighted coverage: add the query with most newly covered grade
	// points per API request. This is a measured heuristic, not a global optimum.
	var selected []int
	current := scoreSearchSet(catalogs, points, selected)
	for {
		best := -1
		var bestScore searchSetScore
		for i, catalog := range catalogs {
			if slices.Contains(selected, i) {
				continue
			}
			next := scoreSearchSet(catalogs, points, append(slices.Clone(selected), i))
			gain := next.GradePoints - current.GradePoints
			if gain <= 0 {
				continue
			}
			if best < 0 || gain*catalogs[best].Requests > (bestScore.GradePoints-current.GradePoints)*catalog.Requests {
				best, bestScore = i, next
			}
		}
		if best < 0 {
			break
		}
		selected = append(selected, best)
		current = bestScore
		// Drop any previously selected query that adds no remaining grade points.
		for i := len(selected) - 1; i >= 0; i-- {
			trial := append(slices.Clone(selected[:i]), selected[i+1:]...)
			score := scoreSearchSet(catalogs, points, trial)
			if score.GradePoints == current.GradePoints {
				selected, current = trial, score
			}
		}
		report.Frontier = append(report.Frontier, current)
	}
	return report
}

func scoreSearchSet(catalogs []studyCatalog, points map[string]int, selected []int) searchSetScore {
	score := searchSetScore{Searches: slices.Clone(selected)}
	seen := map[string]bool{}
	for _, index := range selected {
		catalog := catalogs[index]
		score.Requests += catalog.Requests
		for _, ingredient := range catalog.Ingredients {
			if seen[ingredient.ProductID] {
				continue
			}
			seen[ingredient.ProductID] = true
			score.Products++
			if point := points[ingredient.ProductID]; point > 0 {
				score.UsefulProducts++
				score.GradePoints += point
			}
		}
	}
	score.ProduceScore = score.GradePoints / 100
	return score
}

// Preserve the starting configuration when rerunning a study after changing defaults.
func studyBaseline(directory, location string) ([]kroger.StaplesSearch, error) {
	filename := filepath.Join(directory, "baseline-"+location+".json")
	baseline := kroger.DefaultStaplesSearches()
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return baseline, writeStudyJSON(filename, baseline)
	}
	if err != nil {
		return nil, err
	}
	baseline = nil
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, err
	}
	return baseline, nil
}
