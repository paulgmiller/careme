package recipes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

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

func stepImageID(hash string, step int) string {
	return "steps/" + hash + "/" + strconv.Itoa(step)
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

func (s *server) startRecipeImageGeneration(ctx context.Context, recipe ai.Recipe) {
	s.wg.Go(func() {
		recipeHash := recipe.ComputeHash()
		// 4 minutes is a magical number here. neeed to look at data.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Minute)
		defer cancel()

		exists, err := s.images.Exists(ctx, recipeHash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to check cached recipe image", "hash", recipeHash, "error", err)
			return
		}
		if exists {
			return
		}
		slog.InfoContext(ctx, "generating new recipe image", "hash", recipeHash)
		image, err := s.imagegen.GenerateRecipeImage(ctx, recipe)
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate recipe image", "hash", recipeHash, "error", err)
			return
		}
		if err := s.images.Save(ctx, recipeHash, image); err != nil {
			slog.ErrorContext(ctx, "failed to save recipe image", "hash", recipeHash, "error", err)
		}
	})
}
