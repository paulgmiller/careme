package recipes

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type postRecipeFunc func(context.Context, ai.Recipe, string, time.Time) error

func (f postRecipeFunc) RecipeReady(ctx context.Context, r ai.Recipe, locationID string, date time.Time) error {
	return f(ctx, r, locationID, date)
}

func TestPostRecipeJobsRunIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imageStarted, wineStarted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		imageErr := errors.New("image failed")
		images := postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error {
			close(imageStarted)
			<-release
			return imageErr
		})
		wine := postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error { close(wineStarted); <-release; return nil })
		done := make(chan error, 1)
		go func() {
			done <- PostRecipeJobs{images, wine}.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}, "70100023", time.Time{})
		}()
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

func TestWineCompletionUsesExplicitInputsAndCachesOptionalFailure(t *testing.T) {
	rio := IO(cache.NewInMemoryCache())
	p := DefaultParams(&locations.Location{ID: "70100023", Name: "Store"}, time.Now())
	// Parameters need not be cached: completion receives store and date directly.
	recipe := ai.Recipe{Title: "Dinner", OriginHash: p.Hash(), WineStyles: []string{"Red"}}
	calls := 0
	picker := wineTestGenerator{pick: func(_ context.Context, location string, r ai.Recipe, date time.Time) (*ai.WineSelection, error) {
		calls++
		assert.Equal(t, p.Location.ID, location)
		assert.True(t, p.Date.Equal(date))
		assert.Equal(t, recipe, r)
		return nil, errors.New("wine unavailable")
	}}
	worker := testWineWorker(rio, picker)
	require.NoError(t, worker.RecipeReady(t.Context(), recipe, p.Location.ID, p.Date))
	selection, err := rio.wineFromCache(t.Context(), recipe.ComputeHash())
	require.NoError(t, err)
	require.Empty(t, selection.Wines)
	require.NoError(t, worker.RecipeReady(t.Context(), recipe, p.Location.ID, p.Date))
	require.Equal(t, 1, calls)
}

func TestBackgroundPostRecipeJobsReturnBeforeWineCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan struct{}), make(chan struct{})

		postJobs := PostRecipeJobs{postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error { close(started); <-release; return nil })}.Background()
		require.NoError(t, postJobs.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}, "70100023", time.Time{}))
		<-started
		close(release)
		postJobs.Wait()
	})
}

func TestPostRecipeJobsCollectsEveryWorkerError(t *testing.T) {
	first, second := errors.New("first worker failed"), errors.New("second worker failed")
	jobs := PostRecipeJobs{
		postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error { return first }),
		postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error { return nil }),
		postRecipeFunc(func(context.Context, ai.Recipe, string, time.Time) error { return second }),
	}
	err := jobs.RecipeReady(t.Context(), ai.Recipe{Title: "Dinner"}, "70100023", time.Time{})
	require.ErrorIs(t, err, first)
	require.ErrorIs(t, err, second)
	require.NoError(t, PostRecipeJobs{}.RecipeReady(t.Context(), ai.Recipe{}, "70100023", time.Time{}))
}

func TestPostRecipeJobsForwardInputs(t *testing.T) {
	recipe := ai.Recipe{Title: "Dinner"}
	date := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	job := postRecipeFunc(func(_ context.Context, got ai.Recipe, location string, gotDate time.Time) error {
		assert.Equal(t, recipe, got)
		assert.Equal(t, "store", location)
		assert.Equal(t, date, gotDate)
		return nil
	})
	jobs := PostRecipeJobs{job}
	require.NoError(t, jobs.RecipeReady(t.Context(), recipe, "store", date))
	background := jobs.Background()
	require.NoError(t, background.RecipeReady(t.Context(), recipe, "store", date))
	background.Wait()
}
