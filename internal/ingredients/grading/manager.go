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
	GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error)
	PrioritizeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error)
}

type Manager interface {
	Service
	Wait()
}

type grader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
}

type rubberstamp struct{}

func (r rubberstamp) GradeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	results := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		item, err := inputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		item.Grade = &ai.IngredientGrade{
			SchemaVersion: "ingredient-grade-disabled",
			Score:         10,
			Reason:        "ingredient grading disabled",
		}
		results = append(results, item)
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

func (m *multiGrader) GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	return m.gradeInParallel(ctx, ingredients)
}

func (m *multiGrader) gradeInParallel(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	type groupedIngredient struct {
		input     ai.InputIngredient
		positions []int
	}

	unique := make([]groupedIngredient, 0, len(ingredients))
	indexByKey := make(map[string]int, len(ingredients))
	for i, ingredient := range ingredients {
		input, err := inputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		key := ingredientKey(input)
		if idx, ok := indexByKey[key]; ok {
			unique[idx].positions = append(unique[idx].positions, i)
			continue
		}
		indexByKey[key] = len(unique)
		unique = append(unique, groupedIngredient{
			input:     input,
			positions: []int{i},
		})
	}

	uniqueResults := make([]ai.InputIngredient, len(unique))
	errs := make([]error, len(unique))
	var batches sync.WaitGroup
	for start := 0; start < len(unique); start += ingredientGradeBatchSize {
		start := start
		end := min(start+ingredientGradeBatchSize, len(unique))
		batches.Go(func() {
			batchIngredients := make([]ai.InputIngredient, 0, end-start)
			for _, item := range unique[start:end] {
				batchIngredients = append(batchIngredients, item.input)
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
			return nil, fmt.Errorf("grade ingredient %q: %w", ingredientLabel(unique[i].input), err)
		}
		if uniqueResults[i].Grade == nil {
			return nil, fmt.Errorf("ingredient grader returned no result for %q", ingredientLabel(unique[i].input))
		}
	}

	results := make([]ai.InputIngredient, len(ingredients))
	for i, item := range unique {
		for _, pos := range item.positions {
			results[pos] = uniqueResults[i]
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
	for i, result := range graded {
		entry := scoredIngredient{
			ingredient: ingredients[i],
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
		return strings.Compare(strings.ToLower(loFromPtr(a.ingredient.Description)), strings.ToLower(loFromPtr(b.ingredient.Description)))
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

func inputIngredientFromKrogerIngredient(ingredient kroger.Ingredient) (ai.InputIngredient, error) {
	item := ai.InputIngredient{
		ProductID:    strings.TrimSpace(loFromPtr(ingredient.ProductId)),
		AisleNumber:  strings.TrimSpace(loFromPtr(ingredient.AisleNumber)),
		Brand:        strings.TrimSpace(loFromPtr(ingredient.Brand)),
		Description:  strings.TrimSpace(loFromPtr(ingredient.Description)),
		Size:         strings.TrimSpace(loFromPtr(ingredient.Size)),
		PriceRegular: priceToString(ingredient.PriceRegular),
		PriceSale:    priceToString(ingredient.PriceSale),
		Categories:   categoriesFromPtr(ingredient.Categories),
	}
	item = ai.NormalizeInputIngredient(item)
	if item.ProductID == "" {
		return ai.InputIngredient{}, fmt.Errorf("ingredient product_id is required for %q", toStr(ingredient.Description)))
	}
	return item, nil
}

func ingredientKey(ingredient ai.InputIngredient) string {
	return cacheKey(ai.NormalizeInputIngredient(ingredient).Hash())
}

func ingredientLabel(ingredient ai.InputIngredient) string {
	if value := strings.TrimSpace(ingredient.Description); value != "" {
		return value
	}
	if value := strings.TrimSpace(ingredient.Brand); value != "" {
		return value
	}
	return strings.TrimSpace(ingredient.ProductID)
}

func loFromPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

urn "unknown ingredient"
}

func categoriesFromPtr(ptr *[]string) []string {
	if ptr == nil {
		return nil
	}
	return append([]string(nil), (*ptr)...)
}

func priceToString(price *float32) string {
	if price == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *price)
}
