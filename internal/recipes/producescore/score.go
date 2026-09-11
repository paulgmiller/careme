package producescore

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	locationtypes "careme/internal/locations/types"
)

const IngredientGradeCutoff = 6

var nowFn = time.Now

type ingredientCache interface {
	IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error)
}

type CachedProduceScorer struct {
	cache        ingredientCache
	locationHash func(locationtypes.Location, time.Time) string
}

func NewCachedProduceScorer(c ingredientCache, locationHash func(locationtypes.Location, time.Time) string) *CachedProduceScorer {
	return &CachedProduceScorer{cache: c, locationHash: locationHash}
}

func (s *CachedProduceScorer) ProduceScore(ctx context.Context, loc locationtypes.Location) *int {
	date, err := locations.StoreToDate(ctx, nowFn(), &loc)
	if err != nil {
		slog.WarnContext(ctx, "bad store date", "zip", loc.ZipCode)
		return nil
	}

	for _, candidate := range []time.Time{date, date.AddDate(0, 0, -1)} {
		ingredients, err := s.cache.IngredientsFromCache(ctx, s.locationHash(loc, candidate))
		if err == nil {
			score := sumIngredientGradesAboveCutoff(ingredients)
			return &score
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			slog.WarnContext(ctx, "failed to read cached produce score ingredients", "location_id", loc.ID, "date", candidate.Format("2006-01-02"), "error", err)
		}
	}

	return nil
}

func sumIngredientGradesAboveCutoff(ingredients []ai.InputIngredient) int {
	score := 0
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil || ingredient.Grade.Score <= IngredientGradeCutoff {
			continue
		}
		score += ingredient.Grade.Score
	}
	return score / 100
}
