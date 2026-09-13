package recipes

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"careme/internal/ai"
	"careme/internal/cache"
)

// Check the recipe's own generation, so a different submitted list hash cannot
// unlock a draft. Completed lists also exclude superseded first attempts.
func (s *server) recipeReviewPending(ctx context.Context, recipe ai.Recipe, listHash string) (bool, error) {
	if recipe.OriginHash != "" {
		listHash = recipe.OriginHash
	}
	hash := recipe.ComputeHash()
	contains := func(recipes []ai.Recipe) bool {
		return slices.ContainsFunc(recipes, func(r ai.Recipe) bool { return r.ComputeHash() == hash })
	}
	list, err := s.FromCache(ctx, listHash)
	if err == nil {
		return !contains(list.Recipes), nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return false, fmt.Errorf("load completed recipe list: %w", err)
	}
	progress, err := s.generationStatuses.Load(ctx, listHash)
	if errors.Is(err, cache.ErrNotFound) {
		// Legacy recipes predate persisted generation status.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load recipe review progress: %w", err)
	}
	for _, slot := range progress.Slots {
		if slot.RecipeHash == hash {
			return !slot.Reviewed, nil
		}
	}
	params, err := s.ParamsFromCache(ctx, listHash)
	if err != nil {
		return false, fmt.Errorf("load saved recipes from generation: %w", err)
	}
	return !contains(params.Saved), nil
}
