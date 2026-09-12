package recipes

import (
	"context"
	"errors"
	"fmt"

	"careme/internal/cache"
	"careme/internal/recipes/status"
)

var errRecipeNotReady = errors.New("recipe not ready or not in shopping list")

// requireReadyRecipe allows selection of recipes in a completed list, recipes
// published in a generation slot, and recipes carried forward in saved params.
func (s *server) requireReadyRecipe(ctx context.Context, listHash, recipeHash string) error {
	list, err := s.FromCache(ctx, listHash)
	if err == nil {
		for _, recipe := range list.Recipes {
			if recipe.ComputeHash() == recipeHash {
				return nil
			}
		}
		return s.requireReadyReplacement(ctx, listHash, recipeHash)
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("load completed list: %w", err)
	}
	progress, err := s.generationStatuses.Load(ctx, listHash)
	if err == nil {
		for _, slot := range progress.Slots {
			if slot.RecipeHash == recipeHash {
				return nil
			}
		}
	} else if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("load generation status: %w", err)
	}
	params, err := s.ParamsFromCache(ctx, listHash)
	if err == nil {
		for _, recipe := range params.Saved {
			if recipe.ComputeHash() == recipeHash {
				return nil
			}
		}
	} else if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("load recipe params: %w", err)
	}
	return errRecipeNotReady
}

// Single-recipe refreshes intentionally do not rewrite their original list.
// Their completed job, rather than the cached draft, proves readiness.
func (s *server) requireReadyReplacement(ctx context.Context, listHash, recipeHash string) error {
	recipe, err := s.SingleFromCache(ctx, recipeHash)
	if errors.Is(err, cache.ErrNotFound) {
		return errRecipeNotReady
	}
	if err != nil {
		return fmt.Errorf("load replacement recipe: %w", err)
	}
	if recipe.OriginHash != listHash || recipe.ParentHash == "" {
		return errRecipeNotReady
	}
	thread, err := s.ThreadFromCache(ctx, recipe.ParentHash)
	if errors.Is(err, cache.ErrNotFound) {
		return errRecipeNotReady
	}
	if err != nil {
		return fmt.Errorf("load replacement thread: %w", err)
	}
	for _, entry := range thread {
		job, err := s.generationStatuses.Load(ctx, status.ID(recipe.ParentHash, entry.ResponseID))
		if errors.Is(err, cache.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load replacement job: %w", err)
		}
		if job.Redirect == recipeHash {
			return nil
		}
	}
	return errRecipeNotReady
}
