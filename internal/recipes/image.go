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

// imageStore reads and writes generated recipe images in their dedicated cache.
type imageStore struct {
	cache cache.Cache
}

// NewimageStore creates a recipe image store backed by c.
func NewImageStore(c cache.Cache) imageStore {
	return imageStore{cache: c}
}

func (iio imageStore) Exists(ctx context.Context, hash string) (bool, error) {
	return iio.cache.Exists(ctx, recipeImageCacheKey(hash))
}

func (iio imageStore) FromCache(ctx context.Context, hash string) (io.ReadCloser, error) {
	return iio.cache.Get(ctx, recipeImageCacheKey(hash))
}

func (iio imageStore) Save(ctx context.Context, hash string, image *ai.GeneratedImage) error {
	if image == nil {
		return fmt.Errorf("recipe image is required")
	}
	if image.Body == nil {
		return fmt.Errorf("recipe image body is required")
	}
	// TODO store content meta data somewher?
	return iio.cache.PutReader(ctx, recipeImageCacheKey(hash), image.Body, cache.Unconditional())
}
