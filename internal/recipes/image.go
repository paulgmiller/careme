package recipes

import (
	"context"
	"fmt"
	"io"

	"careme/internal/ai"
	"careme/internal/cache"
)

const recipeImagesCachePrefix = "recipe_images/"

func recipeImageCacheKey(hash string) string {
	return recipeImagesCachePrefix + hash
}

func (rio recipeio) RecipeImageExists(ctx context.Context, hash string) (bool, error) {
	return rio.Cache.Exists(ctx, recipeImageCacheKey(hash))
}

func (rio recipeio) RecipeImageFromCache(ctx context.Context, hash string) (io.ReadCloser, error) {
	return rio.Cache.Get(ctx, recipeImageCacheKey(hash))
}

func (rio recipeio) SaveRecipeImage(ctx context.Context, hash string, image *ai.GeneratedImage) error {
	if image == nil {
		return fmt.Errorf("recipe image is required")
	}
	if image.Body == nil {
		return fmt.Errorf("recipe image body is required")
	}
	// TODO store content meta data somewher?
	return rio.Cache.PutReader(ctx, recipeImageCacheKey(hash), image.Body, cache.Unconditional())
}
