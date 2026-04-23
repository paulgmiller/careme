package grading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
)

const (
	defaultMinimumIngredients = 24
	ingredientGradeBatchSize  = 30
)

type Service interface {
	GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error)
	PrioritizeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error)
}

type Manager interface {
	Service
	Wait()
}

type grader interface {
	GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error)
}

type rubberstamp struct{}

func (r rubberstamp) GradeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error) {
	results := make([]ai.GradedIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		results = append(results, ai.GradedIngredient{
			Ingredient: ingredient,
			Grade: &ai.IngredientGrade{
				SchemaVersion: "ingredient-grade-disabled",
				Score:         10,
				Reason:        "ingredient grading disabled",
				Ingredient:    ai.SnapshotFromKrogerIngredient(ingredient),
			},
		})
	}
	return results, nil
}

func (r rubberstamp) PrioritizeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	return slices.Clone(ingredients), nil
}

func (r rubberstamp) Wait() {}

type multiGrader struct {
	grader    grader
	threshold int
	minimum   int
}

func NewManager(cfg *config.Config, c cache.ListCache) Manager {
	if cfg == nil || !cfg.IngredientGrading.Enable || strings.TrimSpace(cfg.AI.APIKey) == "" {
		return rubberstamp{}
	}
	base := ai.NewIngredientGrader(cfg.AI.APIKey, cfg.IngredientGrading.Model)
	return &multiGrader{
		grader:    newCachingGrader(base, NewStore(c)),
		threshold: cfg.IngredientGrading.NormalizedThreshold(),
		minimum:   defaultMinimumIngredients,
	}
}

func (m *multiGrader) Wait() {}

func (m *multiGrader) GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error) {
	return m.gradeInParallel(ctx, ingredients)
}

func (m *multiGrader) gradeInParallel(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.GradedIngredient, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	type groupedIngredient struct {
		ingredient kroger.Ingredient
		positions  []int
	}

	unique := make([]groupedIngredient, 0, len(ingredients))
	indexByKey := make(map[string]int, len(ingredients))
	for i, ingredient := range ingredients {
		key := ingredientKey(ingredient)
		if idx, ok := indexByKey[key]; ok {
			unique[idx].positions = append(unique[idx].positions, i)
			continue
		}
		indexByKey[key] = len(unique)
		unique = append(unique, groupedIngredient{
			ingredient: ingredient,
			positions:  []int{i},
		})
	}

	uniqueResults := make([]ai.GradedIngredient, len(unique))
	errs := make([]error, len(unique))
	var batches sync.WaitGroup
	for start := 0; start < len(unique); start += ingredientGradeBatchSize {
		start := start
		end := min(start+ingredientGradeBatchSize, len(unique))
		batches.Go(func() {
			batchIngredients := make([]kroger.Ingredient, 0, end-start)
			for _, item := range unique[start:end] {
				batchIngredients = append(batchIngredients, item.ingredient)
			}

			graded, err := m.grader.GradeIngredients(ctx, batchIngredients)
			if err != nil {
				for i := range batchIngredients {
					errs[start+i] = err
				}
				return
			}
			if len(graded) != len(batchIngredients) {
				err := fmt.Errorf("ingredient grader returned %d results for batch of %d", len(graded), len(batchIngredients))
				for i := range batchIngredients {
					errs[start+i] = err
				}
				return
			}
			copy(uniqueResults[start:end], graded)
		})
	}
	batches.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("grade ingredient %q: %w", ingredientLabel(unique[i].ingredient), err)
		}
		if uniqueResults[i].Grade == nil {
			return nil, fmt.Errorf("ingredient grader returned no result for %q", ingredientLabel(unique[i].ingredient))
		}
	}

	results := make([]ai.GradedIngredient, len(ingredients))
	for i, item := range unique {
		for _, pos := range item.positions {
			results[pos] = ai.GradedIngredient{
				Ingredient: ingredients[pos],
				Grade:      uniqueResults[i].Grade,
			}
		}
	}
	return results, nil
}

func (m *multiGrader) PrioritizeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	type scoredIngredient struct {
		ingredient kroger.Ingredient
		score      int
		keep       bool
	}

	graded, err := m.GradeIngredients(ctx, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to grade ingredients", "error", err)
		return slices.Clone(ingredients), nil
	}

	scored := make([]scoredIngredient, 0, len(graded))
	for _, result := range graded {
		entry := scoredIngredient{
			ingredient: result.Ingredient,
			score:      10,
			keep:       true,
		}
		if result.Grade != nil {
			entry.score = result.Grade.Score
			entry.keep = result.Grade.Score >= m.threshold
		}
		scored = append(scored, entry)
	}

	slices.SortFunc(scored, func(a, b scoredIngredient) int {
		if a.score != b.score {
			return b.score - a.score
		}
		return strings.Compare(strings.ToLower(ingredientLabel(a.ingredient)), strings.ToLower(ingredientLabel(b.ingredient)))
	})

	prioritized := make([]kroger.Ingredient, 0, len(scored))
	for _, item := range scored {
		if item.keep {
			prioritized = append(prioritized, item.ingredient)
		}
	}
	if len(prioritized) < m.minimum {
		for _, item := range scored {
			if item.keep {
				continue
			}
			prioritized = append(prioritized, item.ingredient)
			if len(prioritized) >= m.minimum {
				break
			}
		}
	}
	if len(prioritized) == 0 {
		return slices.Clone(ingredients), errors.New("ingredient prioritization removed every ingredient")
	}
	return prioritized, nil
}

func ingredientKey(ingredient kroger.Ingredient) string {
	snapshot := ai.SnapshotFromKrogerIngredient(ingredient)
	return cacheKey(snapshot.Hash())
}

func ingredientLabel(ingredient kroger.Ingredient) string {
	if value := strings.TrimSpace(loFromPtr(ingredient.Description)); value != "" {
		return value
	}
	if value := strings.TrimSpace(loFromPtr(ingredient.Brand)); value != "" {
		return value
	}
	return strings.TrimSpace(loFromPtr(ingredient.ProductId))
}

func loFromPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
