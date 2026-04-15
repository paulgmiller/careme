package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
)

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
	Ready(ctx context.Context) error
}

// move critique.go over here?
type critiqueCache interface {
	CritiqueFromCache(ctx context.Context, hash string) (*ai.RecipeCritique, error)
	SaveCritique(ctx context.Context, hash string, critique *ai.RecipeCritique) error
}

var _ recipeCritiquer = &cachingCritiquer{}

type cachingCritiquer struct {
	critiquer recipeCritiquer
	cache     critiqueCache
}

func newCachingCritiquer(critiquer recipeCritiquer, cache cache.Cache) *cachingCritiquer {
	if critiquer == nil || cache == nil {
		panic("critiquer and cache must not be nil")
	}
	return &cachingCritiquer{
		critiquer: critiquer,
		cache:     IO(cache),
	}
}

func (c *cachingCritiquer) Ready(ctx context.Context) error {
	return c.critiquer.Ready(ctx)
}

func (c *cachingCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	hash := recipe.ComputeHash()
	critique, err := c.cache.CritiqueFromCache(ctx, hash)
	if err == nil {
		return critique, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load cached recipe critique", "recipe", recipe.Title, "hash", hash, "error", err)
		return nil, fmt.Errorf("load cached critique for recipe %q (%s): %w", recipe.Title, hash, err)
	}

	critique, err = c.critiquer.CritiqueRecipe(ctx, recipe)
	if err != nil {
		return nil, err
	}
	if err := c.cache.SaveCritique(ctx, hash, critique); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe critique", "recipe", recipe.Title, "hash", hash, "error", err)
		// not actually fatal just erro?
	}
	return critique, nil
}
