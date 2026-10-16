package gradereview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
)

type locationLookup interface {
	GetLocationByID(context.Context, string) (*locations.Location, error)
}
type ingredientCache interface {
	IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error)
}

type CachedCatalog struct {
	locations   locationLookup
	ingredients ingredientCache
	now         func() time.Time
}

func NewCachedCatalog(locations locationLookup, ingredients ingredientCache) *CachedCatalog {
	return &CachedCatalog{locations: locations, ingredients: ingredients, now: time.Now}
}

// LoadCatalog uses today's cached ingredients, or yesterday's on a cache miss,
// matching the produce score's store-day lookup. It never fetches or grades items.
func (c *CachedCatalog) LoadCatalog(ctx context.Context, id string) (*locations.Location, []ai.InputIngredient, error) {
	location, err := c.locations.GetLocationByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("load store %q: %w", id, err)
	}
	date, err := locations.StoreToDate(ctx, c.now(), location)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve store date: %w", err)
	}
	for _, candidate := range []time.Time{date, date.AddDate(0, 0, -1)} {
		ingredients, err := c.ingredients.IngredientsFromCache(ctx, cachekey.ForStore(location.ID, candidate, cachekey.StaplesSignature(location.ID)))
		if err == nil {
			return location, ingredients, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, nil, fmt.Errorf("load cached catalog for %q: %w", id, err)
		}
	}
	return nil, nil, fmt.Errorf("load cached catalog for %q: %w", id, cache.ErrNotFound)
}
