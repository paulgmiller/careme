package recipes

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
)

const recipeImagesCachePrefix = "recipe_images/"

func recipeImageCacheKey(hash string) string {
	return recipeImagesCachePrefix + hash + "." + recipeImageExtension(ai.RecipeImageContentType())
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
	if image.Body == nil {
		return fmt.Errorf("recipe image body is required")
	}
	if image.ContentType != ai.RecipeImageContentType() {
		return fmt.Errorf("recipe image content type %q does not match configured type %q", image.ContentType, ai.RecipeImageContentType())
	}
	return rio.Cache.PutReader(ctx, recipeImageCacheKey(hash), image.Body, cache.Unconditional())
}

func recipeImageExtension(contentType string) string {
	switch strings.TrimSpace(strings.ToLower(contentType)) {
	case "image/webp":
		return "webp"
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	default:
		return "bin"
	}
}
