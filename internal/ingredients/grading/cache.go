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

// Catalogs can contain thousands of ingredients, with several stores grading
// concurrently. Bound cache traffic across all calls on the shared grader.
const ingredientGradeCacheConcurrency = 16

type baseGrader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
	CacheVersion() string
}

type cachingGrader struct {
	cacheVersion string
	grader       baseGrader
	store        store
	cacheSlots   chan struct{}
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
		cacheSlots:   make(chan struct{}, ingredientGradeCacheConcurrency),
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
		if err := c.acquireCacheSlot(ctx); err != nil {
			return lookupResult{}, fmt.Errorf("load cached ingredient grade for %q: %w", ingredientLabel(ingredient), err)
		}
		gradedIngredient, err := c.store.Load(ctx, key)
		<-c.cacheSlots
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

	// Cache successful results without making persistence failures fail grading.
	var wg sync.WaitGroup
	for _, gradedIngredient := range gradedIngredients {
		results = append(results, gradedIngredient)
		if gradedIngredient.Grade == nil {
			continue
		}
		wg.Go(func() {
			ctx := context.WithoutCancel(ctx)
			// Writes are optional, but share the read limit so completed grades
			// cannot overwhelm storage while another catalog is loading.
			c.cacheSlots <- struct{}{}
			defer func() { <-c.cacheSlots }()
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

func (c *cachingGrader) acquireCacheSlot(ctx context.Context) error {
	select {
	case c.cacheSlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
