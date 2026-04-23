package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
)

var _ grader = &cachingGrader{}

type baseGrader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
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

func (c *cachingGrader) GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	results := make([]ai.InputIngredient, len(ingredients))
	missingIndices := make([]int, 0, len(ingredients))
	missingIngredients := make([]ai.InputIngredient, 0, len(ingredients))

	for i, ingredient := range ingredients {
		key := ingredientKey(ingredient)
		gradedIngredient, err := c.store.Load(ctx, key)
		if err == nil {
			results[i] = *gradedIngredient
			continue
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load cached ingredient grade", "key", key, "error", err)
			return nil, fmt.Errorf("load cached ingredient grade for %q: %w", ingredientLabel(ingredient), err)
		}
		missingIndices = append(missingIndices, i)
		missingIngredients = append(missingIngredients, ingredient)
	}

	if len(missingIngredients) == 0 {
		return results, nil
	}

	gradedIngredients, err := c.grader.GradeIngredients(ctx, missingIngredients)
	if err != nil {
		return nil, err
	}
	if len(gradedIngredients) != len(missingIngredients) {
		return nil, fmt.Errorf("ingredient grader returned %d ingredients for %d inputs", len(gradedIngredients), len(missingIngredients))
	}
	for i, gradedIngredient := range gradedIngredients {
		ingredient := missingIngredients[i]
		if gradedIngredient.Grade == nil {
			return nil, fmt.Errorf("ingredient grader returned nil grade for %q", ingredientLabel(ingredient))
		}
		key := ingredientKey(ingredient)
		if err := c.store.Save(ctx, key, &gradedIngredient); err != nil {
			slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(ingredient), "error", err)
		}
		results[missingIndices[i]] = gradedIngredient
	}

	return results, nil
}
