package recipes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/ai"
	recipestatus "careme/internal/recipes/status"
)

const staplesRetryBudget = 5 * time.Minute

type staplesRetryPolicy struct {
	budget time.Duration
	delays []time.Duration
	wait   func(context.Context, time.Duration) error
}

type staplesUnavailableError struct {
	attempts int
	err      error
}

func (e *staplesUnavailableError) Error() string {
	return fmt.Sprintf("staples unavailable after %d attempts: %v", e.attempts, e.err)
}

func (e *staplesUnavailableError) Unwrap() error {
	return e.err
}

func defaultStaplesRetryPolicy() staplesRetryPolicy {
	return staplesRetryPolicy{
		budget: staplesRetryBudget,
		delays: []time.Duration{
			10 * time.Second,
			30 * time.Second,
			60 * time.Second,
		},
		wait: waitForStaplesRetry,
	}
}

func waitForStaplesRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (g *generatorService) fetchStaples(ctx context.Context, hash string, p *generatorParams) ([]ai.InputIngredient, error) {
	retryCtx, cancel := context.WithTimeout(ctx, g.staplesRetry.budget)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= len(g.staplesRetry.delays)+1; attempt++ {
		ingredients, err := g.staples.FetchStaples(retryCtx, p)
		if err == nil {
			return ingredients, nil
		}
		lastErr = err

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		willRetry := attempt <= len(g.staplesRetry.delays) && retryCtx.Err() == nil
		slog.WarnContext(ctx, "failed to fetch staples", "attempt", attempt, "will_retry", willRetry, "error", err)
		if !willRetry {
			return nil, &staplesUnavailableError{attempts: attempt, err: lastErr}
		}

		if attempt == 1 {
			g.writeStatus(ctx, hash, recipestatus.StaplesRetrying())
		}
		if err := g.staplesRetry.wait(retryCtx, g.staplesRetry.delays[attempt-1]); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &staplesUnavailableError{attempts: attempt, err: lastErr}
		}
	}

	panic("unreachable staples retry loop")
}
