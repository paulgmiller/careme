package recipes

import (
	"context"
	"fmt"

	"careme/internal/cache"
)

// CockroachStorage owns recipe JSON storage, including shopping lists, params,
// selections, and feedback. It owns its SQL pool and must be closed when finished.
type CockroachStorage struct {
	recipeio
	*cache.CockroachCache
}

var _ cache.ListCache = (*CockroachStorage)(nil)

func NewCockroachStorage(ctx context.Context, databaseURL string) (*CockroachStorage, error) {
	c, err := cache.OpenCockroachCache(ctx, databaseURL, "recipes")
	if err != nil {
		return nil, fmt.Errorf("open recipe storage: %w", err)
	}
	return &CockroachStorage{recipeio: IO(c), CockroachCache: c}, nil
}
