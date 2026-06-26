package critique

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"

	"go.opentelemetry.io/otel"
)

const MinimumRecipeScore = 8

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
	Ready(ctx context.Context) error
}

type rubberstamp struct{}

func (r rubberstamp) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	return &ai.RecipeCritique{OverallScore: 10}, nil
}

func (r rubberstamp) Wait()                           {}
func (r rubberstamp) Ready(ctx context.Context) error { return nil }

func NewMock(c cache.ListCache) *cachingCritiquer {
	return newCachingCritiquer(rubberstamp{}, NewStore(c))
}

type waitingCritiquer struct {
	critiquer recipeCritiquer
	wg        sync.WaitGroup
}

var _ recipeCritiquer = &waitingCritiquer{}

func NewManager(cfg *config.Config, c cache.ListCache, httpClient *http.Client) *waitingCritiquer {
	if !cfg.Gemini.IsEnabled() {
		panic("gemini must be enabled")
	}
	crit := ai.NewCritiquer(cfg.Gemini.APIKey, cfg.Gemini.CritiqueModel, httpClient)
	return &waitingCritiquer{
		critiquer: newCachingCritiquer(crit, NewStore(c)),
	}
}

func (mc *waitingCritiquer) Ready(ctx context.Context) error {
	return mc.critiquer.Ready(ctx)
}

var tracer = otel.Tracer("careme/internal/recipes/critiques")

func (mc *waitingCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	mc.wg.Add(1)
	defer mc.wg.Done()
	ctx, span := tracer.Start(ctx, "critques.recipe")
	defer span.End()
	return mc.critiquer.CritiqueRecipe(ctx, recipe)
}

func (mc *waitingCritiquer) Wait() {
	mc.wg.Wait()
}

func RetryInstructions(c ai.RecipeCritique) []string {
	return []string{
		"Revise recipe. Description should focus on selling the dish not these corrections.",
		fmt.Sprintf("scored %d/10.\n Issues: %s\n Suggested fixes: %s",
			c.OverallScore,
			formatIssues(c.Issues),
			formatSuggestedFixes(c.SuggestedFixes)),
	}
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
