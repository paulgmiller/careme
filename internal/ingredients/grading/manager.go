package grading

import (
	"context"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	ingredientGradeBatchSize = 30
)

type grader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
}

type rubberstamp struct{}

func (r rubberstamp) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	results := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		ingredient.Grade = &ai.IngredientGrade{
			Score:  10,
			Reason: "ingredient grading disabled",
		}
		results = append(results, ingredient)
	}
	return results, nil
}

type multiGrader struct {
	grader grader
}

func NewManager(cfg *config.Config, c cache.ListCache) grader {
	if cfg == nil || !cfg.IngredientGrading.Enable || strings.TrimSpace(cfg.AI.APIKey) == "" {
		return rubberstamp{}
	}
	base := ai.NewIngredientGrader(cfg.AI.APIKey, cfg.IngredientGrading.Model)
	return &multiGrader{
		grader: newCachingGrader(base, NewStore(c)),
	}
}

func (m *multiGrader) GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	// we assume dedupe before thing come in here

	batches := lo.Chunk(ingredients, ingredientGradeBatchSize)
	graded, err := parallelism.Flatten(batches, func(batch []ai.InputIngredient) ([]ai.InputIngredient, error) {
		return m.grader.GradeIngredients(ctx, batch)
	})
	if err != nil {
		// will have cached these
		return nil, err
	}
	return graded, nil
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
