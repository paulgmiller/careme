package critique

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"

	"go.opentelemetry.io/otel"
)

const MinimumRecipeScore = 8

type Result struct {
	Recipe   *ai.Recipe
	Critique *ai.RecipeCritique
	Err      error
}

type Service interface {
	CritiqueRecipe(ctx context.Context, recipes ai.Recipe) <-chan Result
}

// if we have web.go make rubbertamp directly this goes away
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

func (r rubberstamp) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) <-chan Result {
	result := make(chan Result, 1)
	result <- Result{
		Critique: &ai.RecipeCritique{OverallScore: 10},
		Recipe:   &recipe,
	}
	close(result)
	return result
}

func (r rubberstamp) Wait()                           {}
func (r rubberstamp) Ready(ctx context.Context) error { return nil }

type multiCritiquer struct {
	critiquer recipeCritiquer
	wg        sync.WaitGroup
}

func NewManager(cfg *config.Config, c cache.ListCache, httpClient *http.Client) Manager {
	if !cfg.Gemini.IsEnabled() {
		return rubberstamp{}
	}
	crit := ai.NewCritiquer(cfg.Gemini.APIKey, cfg.Gemini.CritiqueModel, httpClient)
	return &multiCritiquer{
		critiquer: newCachingCritiquer(crit, NewStore(c)),
	}
}

func (mc *multiCritiquer) Ready(ctx context.Context) error {
	return mc.critiquer.Ready(ctx)
}

var tracer = otel.Tracer("careme/internal/recipes/critiques")

func (mc *multiCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) <-chan Result {
	result := make(chan Result)
	mc.wg.Go(func() {
		defer close(result)
		ctx, span := tracer.Start(ctx, "critques.recipe")
		defer span.End()
		critique, err := mc.critiquer.CritiqueRecipe(ctx, recipe)
		result <- Result{
			Recipe:   &recipe,
			Critique: critique,
			Err:      err,
		}
	})

	return result
}

func (mc *multiCritiquer) Wait() {
	mc.wg.Wait()
}

func RetryInstructions(result Result) []string {
	return []string{"Revise recipe. Description should focus on selling the dish not these corrections.",
		fmt.Sprintf("scored %d/10.\n Issues: %s\n Suggested fixes: %s",
			result.Critique.OverallScore,
			formatIssues(result.Critique.Issues),
			formatSuggestedFixes(result.Critique.SuggestedFixes)),
	}
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
