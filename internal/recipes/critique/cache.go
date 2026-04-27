package critique

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
)

var _ recipeCritiquer = &cachingCritiquer{}

type cachingCritiquer struct {
	critiquer recipeCritiquer
	store     store
}

func newCachingCritiquer(critiquer recipeCritiquer, store store) *cachingCritiquer {
	if critiquer == nil {
		panic("critiquer must not be nil")
	}
	return &cachingCritiquer{
		critiquer: critiquer,
		store:     store,
	}
}

func (c *cachingCritiquer) Ready(ctx context.Context) error {
	return c.critiquer.Ready(ctx)
}

func (c *cachingCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (critique *ai.RecipeCritique, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes/critique", "recipes.critique.cache")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.Int("recipe.instruction_count", len(recipe.Instructions)))

	hash := recipe.ComputeHash()
	critique, err = c.store.Load(ctx, hash)
	if err == nil {
		span.SetAttributes(attribute.Bool("cache.hit", true))
		return critique, nil
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))
	if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load cached recipe critique", "recipe", recipe.Title, "hash", hash, "error", err)
		return nil, fmt.Errorf("load cached critique for recipe %q (%s): %w", recipe.Title, hash, err)
	}

	critique, err = c.critiquer.CritiqueRecipe(ctx, recipe)
	if err != nil {
		return nil, err
	}
	if err := c.store.Save(ctx, hash, critique); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe critique", "recipe", recipe.Title, "hash", hash, "error", err)
	}
	return critique, nil
}
