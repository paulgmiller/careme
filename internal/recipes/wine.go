package recipes

import (
	"careme/internal/cache"
	"context"
	"io"
	"log/slog"
)

const wineRecommendationsCachePrefix = "wine_recommendations/"

func recipeWineCacheKey(hash string) string {
	return wineRecommendationsCachePrefix + hash
}

func (rio recipeio) WineFromCache(ctx context.Context, hash string) (string, error) {
	return rio.readStringFromCache(ctx, recipeWineCacheKey(hash))
}

func (rio recipeio) SaveWine(ctx context.Context, hash string, recommendation string) error {
	return rio.Cache.Put(ctx, recipeWineCacheKey(hash), recommendation, cache.Unconditional())
}

func (rio recipeio) readStringFromCache(ctx context.Context, key string) (string, error) {
	reader, err := rio.Cache.Get(ctx, key)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached string reader", "key", key, "error", err)
		}
	}()

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
