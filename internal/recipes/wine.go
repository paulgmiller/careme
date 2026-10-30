package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const wineRecommendationsCachePrefix = "wine_recommendations/"

func recipeWineCacheKey(hash string) string {
	return wineRecommendationsCachePrefix + hash
}

func (rio recipeio) wineFromCache(ctx context.Context, hash string) (*ai.WineSelection, error) {
	wineReader, err := rio.Cache.Get(ctx, recipeWineCacheKey(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = wineReader.Close()
	}()
	var selection ai.WineSelection
	err = json.NewDecoder(wineReader).Decode(&selection)
	return &selection, err
}

func (rio recipeio) wineExists(ctx context.Context, hash string) (bool, error) {
	return rio.Cache.Exists(ctx, recipeWineCacheKey(hash))
}

func (rio recipeio) saveWine(ctx context.Context, hash string, selection *ai.WineSelection) error {
	if selection == nil {
		return fmt.Errorf("wine selection is required")
	}
	body, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, recipeWineCacheKey(hash), string(body), cache.Unconditional())
}

// waitForWine polls shared storage, so a pick may finish on any server.
// A saved selection with no wines is complete; only a missing record is retried.
func (rio recipeio) waitForWine(ctx context.Context, hash string) (*ai.WineSelection, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for wine pick %q: %w", hash, err)
		}
		selection, err := rio.wineFromCache(ctx, hash)
		if err == nil {
			return selection, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("load wine pick %q: %w", hash, err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for wine pick %q: %w", hash, ctx.Err())
		case <-ticker.C:
		}
	}
}
