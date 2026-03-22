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
	return recipeImagesCachePrefix + ai.RecipeImageSignature() + "/" + hash + ".png"
}

func (rio recipeio) RecipeImageExists(ctx context.Context, hash string) (bool, error) {
	return rio.Cache.Exists(ctx, recipeImageCacheKey(hash))
}

func (rio recipeio) RecipeImageFromCache(ctx context.Context, hash string) ([]byte, error) {
	return rio.readBytesFromCache(ctx, recipeImageCacheKey(hash))
}

func (rio recipeio) SaveRecipeImage(ctx context.Context, hash string, image *ai.GeneratedImage) error {
	if image == nil {
		return fmt.Errorf("recipe image is required")
	}
	if len(image.Bytes) == 0 {
		return fmt.Errorf("recipe image bytes are required")
	}
	return rio.Cache.PutWriter(ctx, recipeImageCacheKey(hash), cache.Unconditional(), func(w io.Writer) error {
		_, err := w.Write(image.Bytes)
		return err
	})
}
