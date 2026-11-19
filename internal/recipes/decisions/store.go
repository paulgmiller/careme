package decisions

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"careme/internal/cache"
	utypes "careme/internal/users/types"
)

const (
	Window = 7 * 24 * time.Hour
	prefix = "recipe_decisions/"
)

type Store struct {
	cache cache.ListCache
}

func NewStore(c cache.ListCache) *Store { return &Store{cache: c} }

func dayPrefix(userID string, date time.Time) string {
	return prefix + userID + "/" + date.UTC().Format("2006-01-02") + "/"
}

// Record stores a cooldown signal, not a taste preference. Independent recipes
// never share a record, so recording one cannot overwrite another decision.
func (s *Store) Record(ctx context.Context, userID string, recipe utypes.Recipe) error {
	body, err := json.Marshal(recipe)
	if err != nil {
		return fmt.Errorf("marshal recipe decision: %w", err)
	}
	if err := s.cache.Put(ctx, dayPrefix(userID, recipe.CreatedAt)+recipe.Hash, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("record recipe decision: %w", err)
	}
	return nil
}

// Recent reads only the UTC day partitions overlapping the cooldown window.
// Expired records remain stored, but are not scanned in subsequent windows.
func (s *Store) Recent(ctx context.Context, userID string, now time.Time) ([]utypes.Recipe, error) {
	cutoff := now.Add(-Window)
	var recipes []utypes.Recipe
	for day := now.UTC(); !day.Before(cutoff); day = day.AddDate(0, 0, -1) {
		keyPrefix := dayPrefix(userID, day)
		keys, err := s.cache.List(ctx, keyPrefix, "")
		if err != nil {
			return nil, fmt.Errorf("list recipe decisions: %w", err)
		}
		slices.Sort(keys)
		for _, key := range keys {
			reader, err := s.cache.Get(ctx, keyPrefix+key)
			if err != nil {
				return nil, fmt.Errorf("read recipe decision: %w", err)
			}
			var recipe utypes.Recipe
			err = json.NewDecoder(reader).Decode(&recipe)
			closeErr := reader.Close()
			if err != nil {
				return nil, fmt.Errorf("decode recipe decision: %w", err)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("close recipe decision: %w", closeErr)
			}
			if recipe.CreatedAt.After(cutoff) {
				recipes = append(recipes, recipe)
			}
		}
	}
	return recipes, nil
}
