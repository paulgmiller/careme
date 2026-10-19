package gradereview

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
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

const catalogTTL = 30 * time.Minute

type catalogEntry struct {
	location    *locations.Location
	ingredients []ai.InputIngredient
}

type CachedCatalog struct {
	mu          sync.Mutex
	entries     map[string]*catalogEntry
	locations   locationLookup
	ingredients ingredientCache
	now         func() time.Time
}

func NewCachedCatalog(locations locationLookup, ingredients ingredientCache) *CachedCatalog {
	return &CachedCatalog{locations: locations, ingredients: ingredients, now: time.Now, entries: make(map[string]*catalogEntry)}
}

// LoadCatalog retains each store catalog for 30 minutes from loading.
// It uses the current store day, or the previous day on a cache miss,
// matching the produce score. It never fetches or grades items.
func (c *CachedCatalog) LoadCatalog(ctx context.Context, id string) (*locations.Location, []ai.InputIngredient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[id]; ok {
		return entry.location, slices.Clone(entry.ingredients), nil
	}
	location, ingredients, err := c.loadCatalog(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	entry := &catalogEntry{location: location, ingredients: ingredients}
	c.entries[id] = entry
	time.AfterFunc(catalogTTL, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.entries, id)
	})
	return location, slices.Clone(ingredients), nil
}

// RemoveReviewed releases matching ingredients in the active store. Request
// snapshots remain valid until those requests finish.
func (c *CachedCatalog) RemoveReviewed(locationID, gradeKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[locationID]; entry != nil {
		entry.ingredients = slices.DeleteFunc(entry.ingredients, func(ingredient ai.InputIngredient) bool {
			return ingredient.Grade != nil && catalogGradeKey(ingredient) == gradeKey
		})
		if len(entry.ingredients) == 0 {
			entry.ingredients = nil
		}
	}
}

func (c *CachedCatalog) loadCatalog(ctx context.Context, id string) (*locations.Location, []ai.InputIngredient, error) {
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
