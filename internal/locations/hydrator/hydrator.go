package hydrator

import (
	"context"
	"sync"

	locationtypes "careme/internal/locations/types"
)

// could take subset of cache

type loader interface {
	Load(context.Context, string) (locationtypes.Location, error)
}

type LazyHydrator struct {
	loader loader
	mu     sync.RWMutex
	byID   map[string]locationtypes.Location
}

func NewLazyHydrator(loader loader) *LazyHydrator {
	return &LazyHydrator{
		loader: loader,
		byID:   make(map[string]locationtypes.Location),
	}
}

func (h *LazyHydrator) Hydrate(ctx context.Context, locationID string) (locationtypes.Location, error) {
	h.mu.RLock()
	loc, ok := h.byID[locationID]
	h.mu.RUnlock()
	if ok {
		return loc, nil
	}

	loc, err := h.loader.Load(ctx, locationID)
	if err != nil {
		return locationtypes.Location{}, err
	}

	h.mu.Lock()
	h.byID[locationID] = loc
	h.mu.Unlock()
	return loc, nil
}
