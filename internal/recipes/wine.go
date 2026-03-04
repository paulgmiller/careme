package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

const wineRecommendationsCachePrefix = "wine_recommendations/"

func recipeWineCacheKey(hash string) string {
	return wineRecommendationsCachePrefix + hash
}

func (rio recipeio) WineFromCache(ctx context.Context, hash string) (*ai.WineSelection, error) {
	body, err := rio.readBytesFromCache(ctx, recipeWineCacheKey(hash))
	if err != nil {
		return nil, err
	}

	var selection ai.WineSelection
	err = json.Unmarshal(body, &selection)
	return &selection, nil
}

func (rio recipeio) SaveWine(ctx context.Context, hash string, selection *ai.WineSelection) error {
	if selection == nil {
		return fmt.Errorf("wine selection is required")
	}
	body, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, recipeWineCacheKey(hash), string(body), cache.Unconditional())
}

func (rio recipeio) readBytesFromCache(ctx context.Context, key string) ([]byte, error) {
	reader, err := rio.Cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached string reader", "key", key, "error", err)
		}
	}()

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return body, nil
}
