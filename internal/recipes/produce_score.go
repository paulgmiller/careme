package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
)

type CachedProduceScorer struct {
	cache ingredientio
}

func NewCachedProduceScorer(c cache.Cache) *CachedProduceScorer {
	return &CachedProduceScorer{cache: IO(c)}
}

func (s *CachedProduceScorer) ProduceScore(ctx context.Context, loc *locations.Location) (*locations.ProduceScore, error) {
	if loc == nil {
		return nil, fmt.Errorf("location is required")
	}

	date, err := StoreToDate(ctx, nowFn(), loc)
	if err != nil {
		return nil, err
	}

	for _, candidate := range []time.Time{date, date.AddDate(0, 0, -1)} {
		params := DefaultParams(loc, candidate)
		ingredients, err := s.cache.IngredientsFromCache(ctx, params.LocationHash())
		if err == nil {
			return &locations.ProduceScore{
				Score: sumIngredientGradesAboveCutoff(ingredients),
				Date:  candidate,
			}, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.WarnContext(ctx, "failed to read cached produce score ingredients", "location_id", loc.ID, "date", candidate.Format("2006-01-02"), "error", err)
		}
	}

	return nil, nil
}

func sumIngredientGradesAboveCutoff(ingredients []ai.InputIngredient) int {
	score := 0
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil || ingredient.Grade.Score <= IngredientGradeCutoff {
			continue
		}
		score += ingredient.Grade.Score
	}
	return score
}
