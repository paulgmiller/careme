package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
)

var _ grader = &cachingGrader{}

type baseGrader interface {
	GradeIngredient(ctx context.Context, ingredient kroger.Ingredient) (*ai.IngredientGrade, error)
	Ready(ctx context.Context) error
}

type cachingGrader struct {
	grader baseGrader
	store  store
}

func newCachingGrader(grader baseGrader, store store) *cachingGrader {
	if grader == nil {
		panic("ingredient grader must not be nil")
	}
	return &cachingGrader{
		grader: grader,
		store:  store,
	}
}

func (c *cachingGrader) Ready(ctx context.Context) error {
	return c.grader.Ready(ctx)
}

func (c *cachingGrader) GradeIngredient(ctx context.Context, key string, ingredient kroger.Ingredient) (*ai.IngredientGrade, error) {
	grade, err := c.store.Load(ctx, key)
	if err == nil {
		return grade, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load cached ingredient grade", "key", key, "error", err)
		return nil, fmt.Errorf("load cached ingredient grade for %q: %w", ingredientLabel(ingredient), err)
	}

	grade, err = c.grader.GradeIngredient(ctx, ingredient)
	if err != nil {
		return nil, err
	}
	if err := c.store.Save(ctx, key, grade); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(ingredient), "error", err)
	}
	return grade, nil
}
