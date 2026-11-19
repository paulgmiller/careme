package recipes

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"careme/internal/ai"
	"careme/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRequiredImagesJoinBackgroundGeneration(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "provider failure"}[fail], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recipe := ai.Recipe{Title: "Dinner"}
				store := NewImageStore(cache.NewInMemoryCache())
				started, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int32
				providerErr := errors.New("provider unavailable")
				worker := NewRecipeImageGenerator(store, imageTestProvider{generate: func(ctx context.Context, _ ai.Recipe) (*ai.GeneratedImage, error) {
					calls.Add(1)
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					if fail {
						return nil, providerErr
					}
					return &ai.GeneratedImage{Body: strings.NewReader("dish-image")}, nil
				}})
				worker.GenerateInBackground(t.Context(), recipe)
				<-started
				done := make(chan error, 1)
				go func() { done <- worker.WaitOn(t.Context(), []ai.Recipe{recipe}) }()
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("required image preparation returned while its image was still generating")
				default:
				}
				close(release)
				err := <-done
				worker.Wait()
				if fail {
					require.ErrorIs(t, err, providerErr)
					assert.ErrorContains(t, err, recipe.Title)
				} else {
					require.NoError(t, err)
					require.NoError(t, worker.WaitOn(t.Context(), []ai.Recipe{recipe}))
				}
				assert.Equal(t, int32(1), calls.Load(), "required images must reuse background work or cached results")
				exists, err := store.Exists(t.Context(), recipe.ComputeHash())
				require.NoError(t, err)
				assert.Equal(t, !fail, exists)
			})
		})
	}
}

func TestRequiredImagesCanCancelWhileBackgroundContinues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recipe := ai.Recipe{Title: "Dinner"}
		started, release := make(chan struct{}), make(chan struct{})
		worker := NewRecipeImageGenerator(NewImageStore(cache.NewInMemoryCache()), imageTestProvider{generate: func(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
			close(started)
			<-release
			return &ai.GeneratedImage{Body: strings.NewReader("dish-image")}, nil
		}})
		worker.GenerateInBackground(t.Context(), recipe)
		<-started
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- worker.WaitOn(ctx, []ai.Recipe{recipe}) }()
		synctest.Wait()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		close(release)
		worker.Wait()
		require.NoError(t, worker.WaitOn(t.Context(), []ai.Recipe{recipe}))
	})
}
