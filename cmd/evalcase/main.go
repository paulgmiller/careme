package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/paulgmiller/kage/pkg/kage"
	"gopkg.in/yaml.v3"
)

type evalCaseStore interface {
	FromCache(context.Context, string) (*ai.ShoppingList, error)
	ParamsFromCache(context.Context, string) (*recipes.GeneratorParams, error)
}

type promptfooTest struct {
	Description string `yaml:"description"`
	Vars        any    `yaml:"vars"`
}

type cliOptions struct {
	Hash       string
	SecretFile string
}

type recipeVars struct {
	MenuPlan ai.MenuPlan `yaml:"menu_plan"`
}

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		log.Fatal(err)
	}
	if err := kage.Load(options.SecretFile); err != nil {
		log.Fatal(err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatal(err)
	}
	if err := writeEvalCases(context.Background(), os.Stdout, recipes.IO(cacheStore), options.Hash); err != nil {
		log.Fatal(err)
	}
}

func parseOptions(args []string, output io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("evalcase", flag.ContinueOnError)
	fs.SetOutput(output)
	hash := fs.String("hash", "", "shopping-list hash to turn into Promptfoo cases")
	secretFile := fs.String("secret-file", "", "encrypted kage environment file to load before reading cache storage")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}

	*hash = strings.TrimSpace(*hash)
	if *hash == "" {
		return cliOptions{}, fmt.Errorf("must provide -hash")
	}
	return cliOptions{Hash: *hash, SecretFile: strings.TrimSpace(*secretFile)}, nil
}

func writeEvalCases(ctx context.Context, out io.Writer, store evalCaseStore, hash string) error {
	tests, err := buildEvalCases(ctx, store, hash)
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	if err := enc.Encode(tests); err != nil {
		return fmt.Errorf("encode Promptfoo test cases: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("finish Promptfoo test cases: %w", err)
	}
	return nil
}

func buildEvalCases(ctx context.Context, store evalCaseStore, hash string) ([]promptfooTest, error) {
	list, err := store.FromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("load shopping list %s: %w", hash, err)
	}
	params, err := store.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("load params %s: %w", hash, err)
	}
	if params.Location == nil {
		return nil, fmt.Errorf("shopping list %s has no location in its params", hash)
	}
	plan := list.Plan
	if plan == nil {
		return nil, fmt.Errorf("shopping list %s has no menu plan", hash)
	}
	responseID := strings.TrimSpace(plan.ResponseID)
	if responseID == "" {
		return nil, fmt.Errorf("shopping list %s menu plan has no response id", hash)
	}
	if len(plan.Plans) == 0 {
		return nil, fmt.Errorf("shopping list %s menu plan has no recipe plans", hash)
	}

	tests := make([]promptfooTest, 0, len(plan.Plans))
	for i, recipePlan := range plan.Plans {
		tests = append(tests, promptfooTest{
			Description: fmt.Sprintf("%s, recipe %d: %s", evalDescription(hash, params.Location, "recipe"), i+1, recipePlan.AnchorIngredient),
			Vars: recipeVars{
				MenuPlan: ai.MenuPlan{
					Plans:              []ai.RecipePlan{recipePlan},
					ChefNoteSuggestion: plan.ChefNoteSuggestion,
					ResponseID:         responseID,
					PromptCacheKey:     plan.PromptCacheKey,
				},
			},
		})
	}
	return tests, nil
}

func evalDescription(hash string, location *locations.Location, kind string) string {
	store := strings.TrimSpace(location.Name)
	if store == "" {
		store = location.ID
	}
	return fmt.Sprintf("%s from %s (%s)", kind, store, hash)
}
