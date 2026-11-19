package recipes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
	"golang.org/x/sync/singleflight"
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

// RecipeImageGenerator owns background dish images shared by recipe generation and edits.
type RecipeImageGenerator struct {
	images   ImageStore
	imagegen ImageGen
	wg       sync.WaitGroup
	inflight singleflight.Group
}

func NewRecipeImageGenerator(images ImageStore, imagegen ImageGen) *RecipeImageGenerator {
	return &RecipeImageGenerator{images: images, imagegen: imagegen}
}

func (s *RecipeImageGenerator) Wait() { s.wg.Wait() }

func (s *RecipeImageGenerator) GenerateInBackground(ctx context.Context, recipe ai.Recipe) {
	s.wg.Go(func() {
		if err := s.Prepare(context.WithoutCancel(ctx), []ai.Recipe{recipe}); err != nil {
			slog.ErrorContext(ctx, "failed to prepare recipe image", "error", err)
		}
	})
}

// Prepare waits for required images, joining any generation already in progress.
// Cached images are reused, including for previously generated shopping lists.
func (s *RecipeImageGenerator) Prepare(ctx context.Context, recipes []ai.Recipe) error {
	_, err := parallelism.MapWithErrors(recipes, func(recipe ai.Recipe) (struct{}, error) {
		hash := recipe.ComputeHash()
		result := s.inflight.DoChan(hash, func() (any, error) {
			ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
			defer cancel()
			exists, err := s.images.Exists(ctx, hash)
			if err != nil {
				return nil, fmt.Errorf("check image cache: %w", err)
			}
			if exists {
				return nil, nil
			}
			image, err := s.imagegen.GenerateRecipeImage(ctx, recipe)
			if err != nil {
				return nil, fmt.Errorf("generate image: %w", err)
			}
			if err := s.images.Save(ctx, hash, image); err != nil {
				return nil, fmt.Errorf("save image: %w", err)
			}
			return nil, nil
		})
		var err error
		select {
		case completed := <-result:
			err = completed.Err
		case <-ctx.Done():
			err = ctx.Err()
		}
		if err != nil {
			return struct{}{}, fmt.Errorf("%q: %w", recipe.Title, err)
		}
		return struct{}{}, nil
	})
	return err
}
