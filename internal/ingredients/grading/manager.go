package grading

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
)

const defaultMinimumIngredients = 24

type Result struct {
	Ingredient kroger.Ingredient
	Grade      *ai.IngredientGrade
	Err        error
}

type Service interface {
	GradeIngredients(ctx context.Context, locationHash string, ingredients []kroger.Ingredient) <-chan Result
	PrioritizeIngredients(ctx context.Context, locationHash string, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error)
}

type Manager interface {
	Service
	Wait()
	Ready(ctx context.Context) error
}

type grader interface {
	GradeIngredient(ctx context.Context, key string, ingredient kroger.Ingredient) (*ai.IngredientGrade, error)
	Ready(ctx context.Context) error
}

type rubberstamp struct{}

func (r rubberstamp) GradeIngredients(_ context.Context, _ string, ingredients []kroger.Ingredient) <-chan Result {
	results := make(chan Result, len(ingredients))
	for _, ingredient := range ingredients {
		results <- Result{
			Ingredient: ingredient,
			Grade: &ai.IngredientGrade{
				SchemaVersion: "ingredient-grade-disabled",
				Score:         10,
				Decision:      ai.IngredientDecisionKeep,
				Reason:        "ingredient grading disabled",
				Ingredient:    ai.SnapshotFromKrogerIngredient(ingredient),
			},
		}
	}
	close(results)
	return results
}

func (r rubberstamp) PrioritizeIngredients(_ context.Context, _ string, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	return slices.Clone(ingredients), nil
}

func (r rubberstamp) Wait()                       {}
func (r rubberstamp) Ready(context.Context) error { return nil }

type multiGrader struct {
	grader    grader
	threshold int
	minimum   int
	wg        sync.WaitGroup
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

func (m *multiGrader) Ready(ctx context.Context) error {
	return m.grader.Ready(ctx)
}

func (m *multiGrader) Wait() {
	m.wg.Wait()
}

func (m *multiGrader) GradeIngredients(ctx context.Context, locationHash string, ingredients []kroger.Ingredient) <-chan Result {
	results := make(chan Result, len(ingredients))
	m.wg.Add(len(ingredients))

	var localWg sync.WaitGroup
	for _, ingredient := range ingredients {
		localWg.Go(func() {
			defer m.wg.Done()
			grade, err := m.grader.GradeIngredient(ctx, ingredientKey(locationHash, ingredient), ingredient)
			results <- Result{
				Ingredient: ingredient,
				Grade:      grade,
				Err:        err,
			}
		})
	}
	go func() {
		localWg.Wait()
		close(results)
	}()
	return results
}

func (m *multiGrader) PrioritizeIngredients(ctx context.Context, locationHash string, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	type scoredIngredient struct {
		ingredient kroger.Ingredient
		score      int
		keep       bool
		err        error
	}

	scored := make([]scoredIngredient, 0, len(ingredients))
	for result := range m.GradeIngredients(ctx, locationHash, ingredients) {
		entry := scoredIngredient{
			ingredient: result.Ingredient,
			score:      10,
			keep:       true,
			err:        result.Err,
		}
		if result.Err != nil {
			slog.ErrorContext(ctx, "failed to grade ingredient", "ingredient", ingredientLabel(result.Ingredient), "error", result.Err)
		} else if result.Grade != nil {
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

func ingredientKey(locationHash string, ingredient kroger.Ingredient) string {
	snapshot := ai.SnapshotFromKrogerIngredient(ingredient)
	return cacheKey(strings.TrimSpace(locationHash), snapshot.Hash())
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
