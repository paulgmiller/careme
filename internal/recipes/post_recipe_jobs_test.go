package recipes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type postRecipeFunc func(context.Context, ai.Recipe) error

func (f postRecipeFunc) RecipeReady(ctx context.Context, r ai.Recipe) error { return f(ctx, r) }

func TestPostRecipeJobsRunIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imageStarted, wineStarted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		imageErr := errors.New("image failed")
		images := NewRecipeImageGenerator(NewImageStore(cache.NewInMemoryCache()), imageTestProvider{generate: func(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
			close(imageStarted)
			<-release
			return nil, imageErr
		}})
		wine := postRecipeFunc(func(context.Context, ai.Recipe) error { close(wineStarted); <-release; return nil })
		done := make(chan error, 1)
		go func() { done <- PostRecipeJobs{images, wine}.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}) }()
		<-imageStarted
		<-wineStarted
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("blocking post-recipe jobs returned before jobs finished")
		default:
		}
		close(release)
		require.ErrorIs(t, <-done, imageErr)
	})
}

func TestWineCompletionUsesOriginParamsAndCachesOptionalFailure(t *testing.T) {
	rio := IO(cache.NewInMemoryCache())
	p := DefaultParams(&locations.Location{ID: "70100023", Name: "Store"}, time.Now())
	require.NoError(t, rio.SaveParams(t.Context(), p))
	recipe := ai.Recipe{Title: "Dinner", OriginHash: p.Hash(), WineStyles: []string{"Red"}}
	calls := 0
	picker := wineTestGenerator{pick: func(_ context.Context, location string, r ai.Recipe, date time.Time) (*ai.WineSelection, error) {
		calls++
		assert.Equal(t, p.Location.ID, location)
		assert.True(t, p.Date.Equal(date))
		assert.Equal(t, recipe, r)
		return nil, errors.New("wine unavailable")
	}}
	worker := recipeWineGenerator{rio, picker}
	require.NoError(t, worker.RecipeReady(t.Context(), recipe))
	selection, err := rio.wineFromCache(t.Context(), recipe.ComputeHash())
	require.NoError(t, err)
	require.Empty(t, selection.Wines)
	require.NoError(t, worker.RecipeReady(t.Context(), recipe))
	require.Equal(t, 1, calls)
}

func TestBackgroundPostRecipeJobsReturnBeforeWineCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})
		images := NewRecipeImageGenerator(NewImageStore(cache.NewInMemoryCache()), imageTestProvider{generate: func(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
			return &ai.GeneratedImage{Body: strings.NewReader("image")}, nil
		}})
		postJobs := PostRecipeJobs{images, postRecipeFunc(func(context.Context, ai.Recipe) error { close(started); <-release; return nil })}.Background()
		require.NoError(t, postJobs.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}))
		<-started
		close(release)
		postJobs.Wait()
	})
}

func TestPostRecipeJobsCollectsEveryWorkerError(t *testing.T) {
	first, second := errors.New("first worker failed"), errors.New("second worker failed")
	jobs := PostRecipeJobs{
		postRecipeFunc(func(context.Context, ai.Recipe) error { return first }),
		postRecipeFunc(func(context.Context, ai.Recipe) error { return nil }),
		postRecipeFunc(func(context.Context, ai.Recipe) error { return second }),
	}
	err := jobs.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"})
	require.ErrorIs(t, err, first)
	require.ErrorIs(t, err, second)
	require.NoError(t, PostRecipeJobs{}.RecipeReady(t.Context(), ai.Recipe{}))
}
