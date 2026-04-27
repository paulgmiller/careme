package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
	"careme/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
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

func (c *cachingGrader) GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) (results []ai.InputIngredient, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/ingredients/grading", "ingredients.grade.cache")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.Int("ingredient.count", len(ingredients)))

	type lookupResult struct {
		cached    *ai.InputIngredient
		missing   *ai.InputIngredient
		pregraded bool
	}

	lookups, err := parallelism.MapWithErrors(ingredients, func(ingredient ai.InputIngredient) (lookupResult, error) {
		if ingredient.Grade != nil {
			return lookupResult{cached: &ingredient, pregraded: true}, nil
		}

		key := cacheKey(c.cacheVersion + "/" + ingredientHash(ingredient))
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

	results = make([]ai.InputIngredient, 0, len(ingredients))
	missingIngredients := make([]ai.InputIngredient, 0, len(ingredients))
	pregradedCount := 0
	for _, lookup := range lookups {
		if lookup.pregraded {
			pregradedCount++
		}
		if lookup.cached != nil {
			results = append(results, *lookup.cached)
			continue
		}
		if lookup.missing != nil {
			missingIngredients = append(missingIngredients, *lookup.missing)
		}
	}
	span.SetAttributes(
		attribute.Int("ingredient.pregraded_count", pregradedCount),
		attribute.Int("ingredient.cache_hit_count", len(results)-pregradedCount),
		attribute.Int("ingredient.cache_miss_count", len(missingIngredients)),
	)

	if len(missingIngredients) == 0 {
		return results, nil
	}

	externalCtx, externalSpan := telemetry.Start(ctx, "careme/internal/ingredients/grading", "ingredients.grade.external")
	externalSpan.SetAttributes(attribute.Int("ingredient.count", len(missingIngredients)))
	gradedIngredients, err := c.grader.GradeIngredients(externalCtx, missingIngredients)
	telemetry.EndResult(externalSpan, err)
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
		key := cacheKey(c.cacheVersion + "/" + ingredientHash(gradedIngredient))
		if err := c.store.Save(ctx, key, &gradedIngredient); err != nil {
			slog.ErrorContext(ctx, "failed to cache ingredient grade", "key", key, "ingredient", ingredientLabel(gradedIngredient), "error", err)
		}
		results = append(results, gradedIngredient)
	}

	return results, nil
}
