package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const mealCount = 10

type recordingCritiquer struct {
	base interface {
		CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
		CritiqueRecipeInBackground(context.Context, ai.Recipe)
	}
	mu     sync.Mutex
	scores map[string]int
}

func newRecordingCritiquer(base interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
	CritiqueRecipeInBackground(context.Context, ai.Recipe)
}) *recordingCritiquer {
	return &recordingCritiquer{base: base, scores: map[string]int{}}
}

func (r *recordingCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	c, err := r.base.CritiqueRecipe(ctx, recipe)
	if err != nil {
		return nil, err
	}
	r.record(recipe, c)
	return c, nil
}

func (r *recordingCritiquer) CritiqueRecipeInBackground(ctx context.Context, recipe ai.Recipe) {
	c, err := r.base.CritiqueRecipe(ctx, recipe)
	if err != nil {
		return
	}
	r.record(recipe, c)
}

func (r *recordingCritiquer) record(recipe ai.Recipe, c *ai.RecipeCritique) {
	if c == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scores[recipe.ComputeHash()] = c.OverallScore
}

func (r *recordingCritiquer) score(recipe ai.Recipe) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	score, ok := r.scores[recipe.ComputeHash()]
	return score, ok
}

type modelResult struct {
	model   string
	recipes []ai.Recipe
	scores  []int
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var storeID, modelsFlag, instructions string
	fs := flag.NewFlagSet("recipebench", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&storeID, "store", "", "store/location ID to generate recipes for")
	fs.StringVar(&modelsFlag, "models", "gpt-5.5,gpt-5.4", "comma-separated recipe models to compare")
	fs.StringVar(&instructions, "instructions", "", "extra cooking notes, like make it vegetarian")
	if err := fs.Parse(args); err != nil {
		return err
	}
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return errors.New("must provide -store")
	}
	models := splitModels(modelsFlag)
	if len(models) != 2 {
		return fmt.Errorf("must provide exactly two models, got %d", len(models))
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	cacheStore, err := newCache(cfg)
	if err != nil {
		return err
	}
	locStore, err := locations.New(cfg, cacheStore, locations.LoadCentroids())
	if err != nil {
		return fmt.Errorf("create location store: %w", err)
	}
	store, err := locStore.GetLocationByID(ctx, storeID)
	if err != nil {
		return fmt.Errorf("load store %s: %w", storeID, err)
	}
	date, err := recipes.StoreToDate(ctx, time.Now(), store)
	if err != nil {
		return fmt.Errorf("resolve store date: %w", err)
	}

	results := make([]modelResult, 0, len(models))
	for _, model := range models {
		result, err := runModel(ctx, cfg, cacheStore, *store, date, model, instructions)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	return writeReport(out, store, date, results)
}

func splitModels(modelsFlag string) []string {
	var models []string
	for _, model := range strings.Split(modelsFlag, ",") {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	return models
}

func newCache(cfg *config.Config) (cache.ListCache, error) {
	if cfg.Mocks.Enable {
		return cache.NewFileCache("recipes"), nil
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}
	return cacheStore, nil
}

func runModel(ctx context.Context, cfg *config.Config, cacheStore cache.ListCache, store locations.Location, date time.Time, model, instructions string) (modelResult, error) {
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, cacheStore, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		return modelResult{}, fmt.Errorf("create staples service: %w", err)
	}
	var baseCritiquer interface {
		CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
		CritiqueRecipeInBackground(context.Context, ai.Recipe)
	}
	baseCritiquer = critique.NewManager(cfg, cacheStore, httpClient)
	if cfg.Mocks.Enable {
		baseCritiquer = critique.NewMock(cacheStore)
	}
	recorder := newRecordingCritiquer(baseCritiquer)
	generator, err := recipes.NewGenerator(ai.NewClient(cfg.AI.APIKey, model, httpClient, prompts.NewCacheRecorder(cacheStore)), recorder, staples, recipes.StatusStore(cacheStore), recipes.IO(cacheStore))
	if err != nil {
		return modelResult{}, err
	}
	params := recipes.DefaultParams(&store, date)
	params.Instructions = instructions
	params.Directive = fmt.Sprintf("Generate %d recipes for model comparison.", mealCount)
	params.Count = mealCount
	shoppingList, err := generator.GenerateRecipes(ctx, params)
	if err != nil {
		return modelResult{}, fmt.Errorf("generate recipes with %s: %w", model, err)
	}
	result := modelResult{model: model, recipes: shoppingList.Recipes}
	for _, recipe := range shoppingList.Recipes {
		if score, ok := recorder.score(recipe); ok {
			result.scores = append(result.scores, score)
		}
	}
	return result, nil
}

func writeReport(w io.Writer, store *locations.Location, date time.Time, results []modelResult) error {
	if _, err := fmt.Fprintf(w, "Recipe model critique benchmark for %s (%s) on %s\n\n", store.Name, store.ID, date.Format("2006-01-02")); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(w, "Model: %s\nRecipes: %d\nAverage critique score: %.2f\n", result.model, len(result.recipes), average(result.scores)); err != nil {
			return err
		}
		for i, recipe := range result.recipes {
			score := "missing"
			if i < len(result.scores) {
				score = fmt.Sprintf("%d", result.scores[i])
			}
			if _, err := fmt.Fprintf(w, "- %s: %s\n", recipe.Title, score); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func average(scores []int) float64 {
	if len(scores) == 0 {
		return 0
	}
	var sum int
	for _, score := range scores {
		sum += score
	}
	return float64(sum) / float64(len(scores))
}
