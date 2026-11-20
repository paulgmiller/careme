package recipes

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
)

type (
	winePicker interface {
		PickAWine(context.Context, string, ai.Recipe, time.Time) (*ai.WineSelection, error)
	}
	recipeWineGenerator struct {
		recipeio
		picker winePicker
	}
)

func NewRecipeWineGenerator(c cache.Cache, client aiClient, staples staplesService) *recipeWineGenerator {
	return &recipeWineGenerator{IO(c), wineSelectionService{client, staples}}
}

type wineSelectionService struct {
	client  aiClient
	staples staplesService
}

func (w wineSelectionService) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	return pickWine(ctx, w.client, w.staples, location, recipe, date)
}

func (w *recipeWineGenerator) RecipeReady(ctx context.Context, recipe ai.Recipe) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	hash := recipe.ComputeHash()
	exists, err := w.wineExists(ctx, hash)
	if err != nil {
		return fmt.Errorf("check wine cache: %w", err)
	}
	if exists {
		return nil
	}
	params, err := w.ParamsFromCache(ctx, recipe.OriginHash)
	if err != nil {
		return fmt.Errorf("load wine recipe params: %w", err)
	}
	selection, err := w.picker.PickAWine(ctx, params.Location.ID, recipe, params.Date)
	if err != nil {
		slog.ErrorContext(ctx, "failed to pick optional wine", "recipe_hash", hash, "error", err)
		selection = &ai.WineSelection{Wines: []ai.Ingredient{}}
	}
	if err := w.saveWine(ctx, hash, selection); err != nil {
		return fmt.Errorf("save wine selection: %w", err)
	}
	return nil
}

// PostRecipeJobs runs independent recipe completion workers concurrently.
type PostRecipeJobs []postRecipe

func (e PostRecipeJobs) RecipeReady(ctx context.Context, recipe ai.Recipe) error {
	_, err := parallelism.MapWithErrors(e, func(job postRecipe) (struct{}, error) { return struct{}{}, job.RecipeReady(ctx, recipe) })
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
func (b *backgroundPostRecipeJobs) RecipeReady(ctx context.Context, recipe ai.Recipe) error {
	b.wg.Go(func() {
		if err := b.PostRecipeJobs.RecipeReady(context.WithoutCancel(ctx), recipe); err != nil {
			slog.ErrorContext(ctx, "failed to run post-recipe jobs", "error", err)
		}
	})
	return nil
}

func NewMockWineGenerator(c cache.Cache) *recipeWineGenerator {
	return &recipeWineGenerator{IO(c), mock{}}
}
