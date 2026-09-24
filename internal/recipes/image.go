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
	recipeSketchCachePrefix = "recipes/sketch/"
)

func recipeImageCacheKey(hash string, style ai.RecipeImageStyle) string {
	if style == ai.RecipeImageSketch {
		return recipeSketchCachePrefix + hash
	}
	return recipeImagesCachePrefix + hash // Existing images are photographs.
}

// imageStore reads and writes generated recipe images in their dedicated cache.
type imageStore struct {
	cache cache.Cache
}

// NewimageStore creates a recipe image store backed by c.
func NewImageStore(c cache.Cache) imageStore {
	return imageStore{cache: c}
}

func (iio imageStore) Exists(ctx context.Context, hash string, style ai.RecipeImageStyle) (bool, error) {
	return iio.cache.Exists(ctx, recipeImageCacheKey(hash, style))
}

func (iio imageStore) FromCache(ctx context.Context, hash string, style ai.RecipeImageStyle) (io.ReadCloser, error) {
	return iio.cache.Get(ctx, recipeImageCacheKey(hash, style))
}

func (iio imageStore) Save(ctx context.Context, hash string, style ai.RecipeImageStyle, image *ai.GeneratedImage) error {
	if image == nil {
		return fmt.Errorf("recipe image is required")
	}
	if image.Body == nil {
		return fmt.Errorf("recipe image body is required")
	}
	// TODO store content meta data somewher?
	return iio.cache.PutReader(ctx, recipeImageCacheKey(hash, style), image.Body, cache.Unconditional())
}
