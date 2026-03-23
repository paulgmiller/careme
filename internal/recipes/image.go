package recipes

import (
	"context"
	"fmt"
	"io"

	"careme/internal/ai"
	"careme/internal/cache"
)

const (
	RecipeImagesContainer   = "images"
	recipeImagesCachePrefix = "recipes/"
)

func recipeImageCacheKey(hash string) string {
	return recipeImagesCachePrefix + hash
}

type imageio struct {
	Cache cache.Cache
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
