package recipes

import (
	"context"
	"fmt"
	"io"
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

// recipeImageGenerator generates and caches dish images.
type recipeImageGenerator struct {
	images   ImageStore
	imagegen ImageGen
}

func NewRecipeImageGenerator(images ImageStore, imagegen ImageGen) *recipeImageGenerator {
	return &recipeImageGenerator{images: images, imagegen: imagegen}
}

// RecipeReady generates and caches the completed recipe's image before returning.
func (s *recipeImageGenerator) RecipeReady(ctx context.Context, recipe ai.Recipe, locationID string, date time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	hash := recipe.ComputeHash()
	exists, err := s.images.Exists(ctx, hash)
	if err != nil {
		return fmt.Errorf("check image cache: %w", err)
	}
	if exists {
		return nil
	}
	image, err := s.imagegen.GenerateRecipeImage(ctx, recipe)
	if err != nil {
		return fmt.Errorf("generate image for %q: %w", recipe.Title, err)
	}
	if err := s.images.Save(ctx, hash, image); err != nil {
		return fmt.Errorf("save image: %w", err)
	}
	return nil
}
