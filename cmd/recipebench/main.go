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
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	mealCount                  = 10
	defaultCritiqueModel       = "gemini-3.1-pro-preview"
	gemini35FlashCritiqueModel = "gemini-3.5-flash"
)

type recipeCritiquer interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

type generationCritiquer interface {
	recipeCritiquer
	CritiqueRecipeInBackground(context.Context, ai.Recipe)
}

type modelResult struct {
	model          string
	recipes        []ai.Recipe
	critiqueScores map[string][]int
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var storeID, modelsFlag, critiqueModelsFlag, instructions string
	fs := flag.NewFlagSet("recipebench", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&storeID, "store", "", "store/location ID to generate recipes for")
	fs.StringVar(&modelsFlag, "models", "gpt-5.5,gpt-5.4", "comma-separated recipe models to compare")
	fs.StringVar(&critiqueModelsFlag, "critique-models", defaultCritiqueModel+","+gemini35FlashCritiqueModel, "comma-separated Gemini critique models to score each recipe with")
	fs.StringVar(&instructions, "instructions", "", "extra cooking notes, like make it vegetarian")
	if err := fs.Parse(args); err != nil {
		return err
	}
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return errors.New("must provide -store")
	}
	recipeModels := splitCSV(modelsFlag)
	if len(recipeModels) != 2 {
		return fmt.Errorf("must provide exactly two recipe models, got %d", len(recipeModels))
	}
	critiqueModels := splitCSV(critiqueModelsFlag)
	if len(critiqueModels) == 0 {
		return errors.New("must provide at least one -critique-models value")
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

	results := make([]modelResult, 0, len(recipeModels))
	for _, model := range recipeModels {
		result, err := runModel(ctx, cfg, cacheStore, *store, date, model, critiqueModels, instructions)
		if err != nil {
			return err
		}
		results = append(results, result)
	}
	return writeReport(out, store, date, critiqueModels, results)
}

func splitCSV(value string) []string {
	var values []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
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

func runModel(ctx context.Context, cfg *config.Config, cacheStore cache.ListCache, store locations.Location, date time.Time, model string, critiqueModels []string, instructions string) (modelResult, error) {
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, cacheStore, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		return modelResult{}, fmt.Errorf("create staples service: %w", err)
	}
	generator, err := recipes.NewGenerator(ai.NewClient(cfg.AI.APIKey, model, httpClient, prompts.NewCacheRecorder(cacheStore)), generationCritiquerForConfig(cfg, cacheStore, httpClient), staples, recipes.StatusStore(cacheStore), recipes.IO(cacheStore))
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
	scores, err := critiqueRecipes(ctx, cfg, httpClient, shoppingList.Recipes, critiqueModels)
	if err != nil {
		return modelResult{}, err
	}
	return modelResult{model: model, recipes: shoppingList.Recipes, critiqueScores: scores}, nil
}

func generationCritiquerForConfig(cfg *config.Config, cacheStore cache.ListCache, httpClient *http.Client) generationCritiquer {
	if cfg.Mocks.Enable {
		return critique.NewMock(cacheStore)
	}
	return critique.NewManager(cfg, cacheStore, httpClient)
}

func critiqueRecipes(ctx context.Context, cfg *config.Config, httpClient *http.Client, recipes []ai.Recipe, critiqueModels []string) (map[string][]int, error) {
	scores := make(map[string][]int, len(critiqueModels))
	for _, critiqueModel := range critiqueModels {
		critiquer := recipeCritiquerForModel(cfg, httpClient, critiqueModel)
		modelScores, err := parallelism.MapWithErrors(recipes, func(recipe ai.Recipe) (int, error) {
			c, err := critiquer.CritiqueRecipe(ctx, recipe)
			if err != nil {
				return 0, fmt.Errorf("critique %q with %s: %w", recipe.Title, critiqueModel, err)
			}
			return c.OverallScore, nil
		})
		if err != nil {
			return nil, err
		}
		scores[critiqueModel] = modelScores
	}
	return scores, nil
}

func recipeCritiquerForModel(cfg *config.Config, httpClient *http.Client, model string) recipeCritiquer {
	if cfg.Mocks.Enable {
		return rubberstampCritiquer{}
	}
	return ai.NewCritiquer(cfg.Gemini.APIKey, model, httpClient)
}

type rubberstampCritiquer struct{}

func (rubberstampCritiquer) CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error) {
	return &ai.RecipeCritique{OverallScore: 10}, nil
}

func writeReport(w io.Writer, store *locations.Location, date time.Time, critiqueModels []string, results []modelResult) error {
	if _, err := fmt.Fprintf(w, "Recipe model critique benchmark for %s (%s) on %s\n", store.Name, store.ID, date.Format("2006-01-02")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Critique models: %s\n\n", strings.Join(critiqueModels, ", ")); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(w, "Recipe model: %s\nRecipes: %d\n", result.model, len(result.recipes)); err != nil {
			return err
		}
		for _, critiqueModel := range critiqueModels {
			if _, err := fmt.Fprintf(w, "Average %s score: %.2f\n", critiqueModel, average(result.critiqueScores[critiqueModel])); err != nil {
				return err
			}
		}
		for i, recipe := range result.recipes {
			if _, err := fmt.Fprintf(w, "- %s", recipe.Title); err != nil {
				return err
			}
			for _, critiqueModel := range critiqueModels {
				score := "missing"
				if scores := result.critiqueScores[critiqueModel]; i < len(scores) {
					score = fmt.Sprintf("%d", scores[i])
				}
				if _, err := fmt.Fprintf(w, " | %s: %s", critiqueModel, score); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
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
