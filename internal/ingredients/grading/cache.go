package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
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
	type lookupResult struct {
		cached  *ai.InputIngredient
		missing *ai.InputIngredient
	}

	lookups, err := parallelism.MapWithErrors(ingredients, func(ingredient ai.InputIngredient) (lookupResult, error) {
		if ingredient.Grade != nil {
			return lookupResult{cached: &ingredient}, nil
		}

		key := ingredientKey(ingredient)
		gradedIngredient, err := c.store.Load(ctx, key)
		if err == nil {
			return lookupResult{cached: gradedIngredient}, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load cached ingredient grade", "key", key, "error", err)
			return lookupResult{}, fmt.Errorf("load cached ingredient grade for %q: %w", ingredientLabel(ingredient), err)
		}
		return lookupResult{missing: &ingredient}, nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]ai.InputIngredient, 0, len(ingredients))
	missingIngredients := make([]ai.InputIngredient, 0, len(ingredients))
	for _, lookup := range lookups {
		if lookup.cached != nil {
			results = append(results, *lookup.cached)
			continue
		}
		if lookup.missing != nil {
			missingIngredients = append(missingIngredients, *lookup.missing)
		}
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

	for _, gradedIngredient := range gradedIngredients {
		if gradedIngredient.Grade == nil {
			return nil, fmt.Errorf("ingredient grader returned nil grade for %q", ingredientLabel(gradedIngredient))
		}
		key := ingredientKey(gradedIngredient)
		if err := c.store.Save(ctx, key, &gradedIngredient); err != nil {
			slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(gradedIngredient), "error", err)
		}
		results = append(results, gradedIngredient)
	}

	return results, nil
}
