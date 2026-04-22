package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const (
	RecipeImagesContainer        = "images"
	recipeImagesCachePrefix      = "recipes/"
	recipeImageStatusCachePrefix = "recipe_image_status/"
)

func recipeImageCacheKey(hash string) string {
	return recipeImagesCachePrefix + hash
}

func recipeImageStatusCacheKey(hash string) string {
	return recipeImageStatusCachePrefix + hash
}

type imageio struct {
	Cache cache.Cache
}

type recipeImageStatus string

const (
	recipeImageStatusPending recipeImageStatus = "pending"
	recipeImageStatusFailed  recipeImageStatus = "failed"
)

type recipeImageStatusRecord struct {
	Status    recipeImageStatus `json:"status"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (iio imageio) RecipeImageExists(ctx context.Context, hash string) (bool, error) {
	return iio.Cache.Exists(ctx, recipeImageCacheKey(hash))
}

func (iio imageio) RecipeImageFromCache(ctx context.Context, hash string) (io.ReadCloser, error) {
	return iio.Cache.Get(ctx, recipeImageCacheKey(hash))
}

func (iio imageio) SaveRecipeImage(ctx context.Context, hash string, image *ai.GeneratedImage) error {
	if image == nil {
		return fmt.Errorf("recipe image is required")
	}
	if image.Body == nil {
		return fmt.Errorf("recipe image body is required")
	}
	// TODO store content meta data somewher?
	return iio.Cache.PutReader(ctx, recipeImageCacheKey(hash), image.Body, cache.Unconditional())
}

func (iio imageio) RecipeImageStatusFromCache(ctx context.Context, hash string) (recipeImageStatusRecord, error) {
	reader, err := iio.Cache.Get(ctx, recipeImageStatusCacheKey(hash))
	if err != nil {
		return recipeImageStatusRecord{}, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close recipe image status reader", "hash", hash, "error", err)
		}
	}()

	var status recipeImageStatusRecord
	if err := json.NewDecoder(reader).Decode(&status); err != nil {
		return recipeImageStatusRecord{}, err
	}
	return status, nil
}

func (iio imageio) SaveRecipeImageStatus(ctx context.Context, hash string, status recipeImageStatusRecord) error {
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe image status: %w", err)
	}
	return iio.Cache.Put(ctx, recipeImageStatusCacheKey(hash), string(body), cache.Unconditional())
}
