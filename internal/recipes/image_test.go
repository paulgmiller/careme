package recipes

import (
	"context"
	"errors"
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/require"

	"careme/internal/ai"
)

type imageTestProvider struct {
	generate func(context.Context, ai.Recipe) (*ai.GeneratedImage, error)
}

func (p imageTestProvider) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	return p.generate(ctx, recipe)
}

func (imageTestProvider) GenerateStepImage(context.Context, ai.Recipe, int) (*ai.GeneratedImage, error) {
	panic("unexpected step image request")
}

func TestBlockingRecipeCompletionReturnsImageFailure(t *testing.T) {
	failure := errors.New("image unavailable")
	worker := NewRecipeImageGenerator(NewImageStore(cache.NewInMemoryCache()), imageTestProvider{generate: func(context.Context, ai.Recipe) (*ai.GeneratedImage, error) { return nil, failure }})
	require.ErrorIs(t, worker.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}), failure)
}
