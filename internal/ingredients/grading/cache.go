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
	GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]*ai.IngredientGrade, error)
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

func (c *cachingGrader) GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error) {
	results := make([]ai.GradedIngredient, len(ingredients))
	missingIndices := make([]int, 0, len(ingredients))
	missingIngredients := make([]kroger.Ingredient, 0, len(ingredients))

	for i, ingredient := range ingredients {
		key := ingredientKey(ingredient)
		grade, err := c.store.Load(ctx, key)
		if err == nil {
			results[i] = ai.GradedIngredient{Ingredient: ingredient, Grade: grade}
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

	grades, err := c.grader.GradeIngredients(ctx, missingIngredients)
	if err != nil {
		return nil, err
	}
	if len(grades) != len(missingIngredients) {
		return nil, fmt.Errorf("ingredient grader returned %d grades for %d ingredients", len(grades), len(missingIngredients))
	}
	for i, grade := range grades {
		ingredient := missingIngredients[i]
		if grade == nil {
			return nil, fmt.Errorf("ingredient grader returned nil grade for %q", ingredientLabel(ingredient))
		}
		key := ingredientKey(ingredient)
		if err := c.store.Save(ctx, key, grade); err != nil {
			slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(ingredient), "error", err)
		}
		results[missingIndices[i]] = ai.GradedIngredient{
			Ingredient: ingredient,
			Grade:      grade,
		}
	}

	return results, nil
}
