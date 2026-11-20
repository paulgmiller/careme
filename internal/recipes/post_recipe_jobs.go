package recipes

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/parallelism"
)

// PostRecipeJobs runs independent recipe completion workers concurrently.
type PostRecipeJobs []postRecipe

func (e PostRecipeJobs) RecipeReady(ctx context.Context, recipe ai.Recipe, locationID string, date time.Time) error {
	_, err := parallelism.MapWithErrors(e, func(job postRecipe) (struct{}, error) {
		return struct{}{}, job.RecipeReady(ctx, recipe, locationID, date)
	})
	return err
}

// Background also runs currentl but just logs erros and doesn't block
func (e PostRecipeJobs) Background() *backgroundPostRecipeJobs {
	return &backgroundPostRecipeJobs{PostRecipeJobs: e}
}

type backgroundPostRecipeJobs struct {
	PostRecipeJobs
	wg sync.WaitGroup
}

func (b *backgroundPostRecipeJobs) Wait() { b.wg.Wait() }
func (b *backgroundPostRecipeJobs) RecipeReady(ctx context.Context, recipe ai.Recipe, locationID string, date time.Time) error {
	b.wg.Go(func() {
		if err := b.PostRecipeJobs.RecipeReady(context.WithoutCancel(ctx), recipe, locationID, date); err != nil {
			slog.ErrorContext(ctx, "failed to run post-recipe jobs", "error", err)
		}
	})
	return nil
}
