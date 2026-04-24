package grading

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
)

const (
	ingredientGradeBatchSize = 30
)

// collapse this soon as we make all staples return []ai.InputIngredient?
type Service interface {
	GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error)
}

type grader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
}

type rubberstamp struct{}

func (r rubberstamp) GradeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	results := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		item, err := InputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		item.Grade = &ai.IngredientGrade{
			Score:  10,
			Reason: "ingredient grading disabled",
		}
		results = append(results, item)
	}
	return results, nil
}

type multiGrader struct {
	grader grader
}

func NewManager(cfg *config.Config, c cache.ListCache) Service {
	if cfg == nil || !cfg.IngredientGrading.Enable || strings.TrimSpace(cfg.AI.APIKey) == "" {
		return rubberstamp{}
	}
	base := ai.NewIngredientGrader(cfg.AI.APIKey, cfg.IngredientGrading.Model)
	return &multiGrader{
		grader: newCachingGrader(base, NewStore(c)),
	}
}

func (m *multiGrader) GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
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
		input, err := InputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		key := ingredientHash(input)
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

func InputIngredientFromKrogerIngredient(ingredient kroger.Ingredient) (ai.InputIngredient, error) {
	item := ai.InputIngredient{
		ProductID:    strings.TrimSpace(toStr(ingredient.ProductId)),
		AisleNumber:  strings.TrimSpace(toStr(ingredient.AisleNumber)),
		Brand:        strings.TrimSpace(toStr(ingredient.Brand)),
		Description:  strings.TrimSpace(toStr(ingredient.Description)),
		Size:         strings.TrimSpace(toStr(ingredient.Size)),
		PriceRegular: clonePrice(ingredient.PriceRegular),
		PriceSale:    clonePrice(ingredient.PriceSale),
		Categories:   categoriesFromPtr(ingredient.Categories),
	}
	item = ai.NormalizeInputIngredient(item)
	if item.ProductID == "" {
		return ai.InputIngredient{}, fmt.Errorf("ingredient product_id is required for %q", toStr(ingredient.Description))
	}
	return item, nil
}

func ingredientHash(ingredient ai.InputIngredient) string {
	return ai.NormalizeInputIngredient(ingredient).Hash()
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

func toStr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func categoriesFromPtr(ptr *[]string) []string {
	if ptr == nil {
		return nil
	}
	return append([]string(nil), (*ptr)...)
}

func clonePrice(price *float32) *float32 {
	if price == nil {
		return nil
	}
	value := *price
	return &value
}
