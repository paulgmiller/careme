package critique

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
)

const MinimumRecipeScore = 8

type Result struct {
	Recipe   *ai.Recipe
	Critique *ai.RecipeCritique
	Err      error
}

type Service interface {
	CritiqueRecipes(ctx context.Context, recipes []ai.Recipe) <-chan Result
}

type Manager interface {
	Service
	Wait()
	Ready(ctx context.Context) error
}

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
	Ready(ctx context.Context) error
}

type rubberstamp struct{}

func (r rubberstamp) CritiqueRecipes(ctx context.Context, recipes []ai.Recipe) <-chan Result {
	results := make(chan Result, len(recipes))
	for _, recipe := range recipes {
		results <- Result{
			Critique: &ai.RecipeCritique{OverallScore: 10},
			Recipe:   &recipe,
		}
	}
	close(results)
	return results
}

func (r rubberstamp) Wait()                           {}
func (r rubberstamp) Ready(ctx context.Context) error { return nil }

type multiCritiquer struct {
	critiquer recipeCritiquer
	wg        sync.WaitGroup
}

func NewManager(cfg *config.Config, c cache.Cache) Manager {
	if !cfg.Gemini.IsEnabled() {
		return rubberstamp{}
	}
	crit := ai.NewCritiquer(cfg.Gemini.APIKey, cfg.Gemini.CritiqueModel)
	return &multiCritiquer{
		critiquer: newCachingCritiquer(crit, NewStore(c)),
	}
}

func (mc *multiCritiquer) Ready(ctx context.Context) error {
	return mc.critiquer.Ready(ctx)
}

func (mc *multiCritiquer) CritiqueRecipes(ctx context.Context, recipes []ai.Recipe) <-chan Result {
	results := make(chan Result, len(recipes))
	mc.wg.Add(len(recipes))

	var localWg sync.WaitGroup
	for _, recipe := range recipes {
		localWg.Go(func() {
			defer mc.wg.Done()
			critique, err := mc.critiquer.CritiqueRecipe(ctx, recipe)
			results <- Result{
				Recipe:   &recipe,
				Critique: critique,
				Err:      err,
			}
		})
	}
	go func() {
		localWg.Wait()
		close(results)
	}()
	return results
}

func (mc *multiCritiquer) Wait() {
	mc.wg.Wait()
}

func RetryInstructions(results []Result) []string {
	revise := fmt.Sprintf("Revise and return exactly %d recipes as replacements for the low-scoring recipes listed below. Description should focus on selling the dish not these corrections", len(results))
	instructions := []string{revise}
	for _, result := range results {
		instructions = append(instructions, fmt.Sprintf(
			"Recipe %q scored %d/10.\n Issues: %s\n Suggested fixes: %s",
			result.Recipe.Title,
			result.Critique.OverallScore,
			formatIssues(result.Critique.Issues),
			formatSuggestedFixes(result.Critique.SuggestedFixes),
		))
	}
	return instructions
}

func Split(ctx context.Context, results <-chan Result, minimumScore int) (accepted []ai.Recipe, retry []Result) {
	for result := range results {
		if result.Err != nil {
			slog.ErrorContext(ctx, "failed to critique recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "error", result.Err)
			accepted = append(accepted, *result.Recipe)
			continue
		}

		if result.Critique.OverallScore >= minimumScore {
			accepted = append(accepted, *result.Recipe)
			continue
		}

		retry = append(retry, result)
	}
	return accepted, retry
}

func formatIssues(issues []ai.RecipeCritiqueIssue) string {
	if len(issues) == 0 {
		return "none listed."
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s", issue.Category, issue.Severity, strings.TrimSpace(issue.Detail)))
	}
	return strings.Join(parts, "; ")
}

func formatSuggestedFixes(fixes []string) string {
	if len(fixes) == 0 {
		return "none listed."
	}
	trimmed := make([]string, 0, len(fixes))
	for _, fix := range fixes {
		if fix = strings.TrimSpace(fix); fix != "" {
			trimmed = append(trimmed, fix)
		}
	}
	if len(trimmed) == 0 {
		return "none listed."
	}
	return strings.Join(trimmed, "; ")
}
