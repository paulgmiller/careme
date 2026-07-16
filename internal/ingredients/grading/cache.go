package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
)

var _ grader = &cachingGrader{}

type baseGrader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
	CacheVersion() string
}

type cachingGrader struct {
	cacheVersion string
	grader       baseGrader
	store        store
}

func newCachingGrader(grader baseGrader, store store) *cachingGrader {
	if grader == nil {
		panic("ingredient grader must not be nil")
	}
	cacheVersion := grader.CacheVersion()
	if cacheVersion == "" {
		panic("ingredient grade cache version must not be empty")
	}
	return &cachingGrader{
		cacheVersion: cacheVersion,
		grader:       grader,
		store:        store,
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

		key := cacheKey(c.cacheVersion + "/" + ingredientHash(ingredient))
		gradedIngredient, err := c.store.Load(ctx, key)
		if err == nil {
			if gradedIngredient.Grade == nil {
				return lookupResult{missing: &ingredient}, nil
			}
			// should probably only cache grade as rest of ingredient may change
			ingredient.Grade = gradedIngredient.Grade
			return lookupResult{cached: &ingredient}, nil
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
	slog.InfoContext(ctx, "grading non cached", "cached", len(results), "missing", len(missingIngredients))

	gradedIngredients, err := c.grader.GradeIngredients(ctx, missingIngredients)

	// might get partial results back save those.
	var wg sync.WaitGroup
	for _, gradedIngredient := range gradedIngredients {
		results = append(results, gradedIngredient)
		if gradedIngredient.Grade == nil {
			continue
		}
		wg.Go(func() {
			ctx := context.WithoutCancel(ctx)
			// could just save grade rather than whole ingredient
			key := cacheKey(c.cacheVersion + "/" + ingredientHash(gradedIngredient))
			if err := c.store.Save(ctx, key, &gradedIngredient); err != nil {
				slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(gradedIngredient), "error", err)
			}
		})
	}
	wg.Wait()
	if err != nil {
		return nil, err
	}

	return results, nil
}
