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

func (c *cachingGrader) GradeIngredients(ctx context.Context, locationHash string, ingredients []kroger.Ingredient) ([]Result, error) {
	results := make([]Result, len(ingredients))
	type missingGroup struct {
		ingredient kroger.Ingredient
		positions  []int
	}
	missing := make(map[string]*missingGroup)

	for i, ingredient := range ingredients {
		key := ingredientKey(locationHash, ingredient)
		grade, err := c.store.Load(ctx, key)
		if err == nil {
			results[i] = Result{Ingredient: ingredient, Grade: grade}
			continue
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load cached ingredient grade", "key", key, "error", err)
			return nil, fmt.Errorf("load cached ingredient grade for %q: %w", ingredientLabel(ingredient), err)
		}
		group, ok := missing[key]
		if !ok {
			group = &missingGroup{ingredient: ingredient}
			missing[key] = group
		}
		group.positions = append(group.positions, i)
	}

	if len(missing) == 0 {
		return results, nil
	}

	keys := make([]string, 0, len(missing))
	batch := make([]kroger.Ingredient, 0, len(missing))
	for key, group := range missing {
		keys = append(keys, key)
		batch = append(batch, group.ingredient)
	}

	for start := 0; start < len(batch); start += ingredientGradeBatchSize {
		end := min(start+ingredientGradeBatchSize, len(batch))
		grades, err := c.grader.GradeIngredients(ctx, batch[start:end])
		if err != nil {
			return nil, err
		}
		if len(grades) != end-start {
			return nil, fmt.Errorf("ingredient grader returned %d grades for batch of %d", len(grades), end-start)
		}
		for i, grade := range grades {
			key := keys[start+i]
			group := missing[key]
			if grade == nil {
				return nil, fmt.Errorf("ingredient grader returned nil grade for %q", ingredientLabel(group.ingredient))
			}
			if err := c.store.Save(ctx, key, grade); err != nil {
				slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(group.ingredient), "error", err)
			}
			for _, pos := range group.positions {
				results[pos] = Result{
					Ingredient: ingredients[pos],
					Grade:      grade,
				}
			}
		}
	}

	return results, nil
}
