package status

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerationProgressUsesJobKind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{
		{name: "interactive", timeout: 10 * time.Minute},
		{name: "campaign", timeout: ai.FlexJobTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := cache.NewInMemoryCache()
				writer, reader := NewStore(c), NewStore(c)
				if tc.name == "interactive" {
					require.NoError(t, writer.Start(t.Context(), "generation", "Planning"))
				} else {
					require.NoError(t, writer.StartBackground(t.Context(), "generation", "Planning"))
				}
				stored, err := reader.load(t.Context(), "generation")
				require.NoError(t, err)
				assert.Equal(t, tc.name == "campaign", stored.Kind == "background")

				time.Sleep(tc.timeout - time.Minute)
				require.NoError(t, writer.Plan(t.Context(), "generation", []ai.RecipePlan{{Cuisine: "Italian"}}))
				require.NoError(t, writer.RecipeDraft(t.Context(), "generation", 0, "draft"))
				require.NoError(t, writer.RecipeReady(t.Context(), "generation", 0, "reviewed"))
				got, err := reader.Load(t.Context(), "generation")
				require.NoError(t, err)
				assert.Empty(t, got.Failed)
				assert.True(t, got.Slots[0].Reviewed)

				time.Sleep(time.Minute)
				got, err = reader.Load(t.Context(), "generation")
				require.NoError(t, err)
				assert.Equal(t, "Recipe generation timed out.", got.Failed)
				assert.Equal(t, "reviewed", got.Slots[0].RecipeHash)
				require.ErrorContains(t, writer.Plan(t.Context(), "generation", nil), "Recipe generation timed out.")
				require.ErrorContains(t, writer.RecipeDraft(t.Context(), "generation", 0, "late"), "Recipe generation timed out.")
				require.ErrorContains(t, writer.RecipeReady(t.Context(), "generation", 0, "late"), "Recipe generation timed out.")
			})
		})
	}
}

func TestGenerationProgressLongTimeoutPreservesTerminalStates(t *testing.T) {
	for _, terminal := range []string{"failed", "completed"} {
		t.Run(terminal, func(t *testing.T) {
			store := NewStore(cache.NewInMemoryCache())
			require.NoError(t, store.StartBackground(t.Context(), "generation", ""))
			require.NoError(t, store.Plan(t.Context(), "generation", []ai.RecipePlan{{}}))
			var reason string
			if terminal == "failed" {
				require.NoError(t, store.Fail(t.Context(), "generation", errors.New("upstream failed")))
				reason = "upstream failed"
			} else {
				require.NoError(t, store.Complete(t.Context(), "generation", "result"))
				reason = "completed with hash result"
			}
			require.ErrorContains(t, store.Plan(t.Context(), "generation", nil), reason)
			require.ErrorContains(t, store.RecipeDraft(t.Context(), "generation", 0, "late"), reason)
			require.ErrorContains(t, store.RecipeReady(t.Context(), "generation", 0, "late"), reason)
		})
	}
}

func TestIDIsStableAndURLSafe(t *testing.T) {
	t.Parallel()
	id := ID("old-hash", "response/id+with=padding")

	require.Len(t, id, 22)
	assert.NotContains(t, id, "+")
	assert.NotContains(t, id, "/")
	assert.NotContains(t, id, "=")
	assert.True(t, IsValidID(id))
	assert.Equal(t, id, ID("old-hash", "response/id+with=padding"))
	assert.NotEqual(t, id, ID("other-hash", "response/id+with=padding"))

	for _, invalid := range []string{"", "not-an-id", id + "=", " " + id} {
		assert.False(t, IsValidID(invalid), invalid)
	}
}

func TestPayloadTimeout(t *testing.T) {
	t.Parallel()
	p := payload{StartedAt: time.Now().Add(-recipeGenerationTimeout - time.Minute)}
	assert.Equal(t, "Recipe generation timed out.", p.failed())
}

func TestPayloadFailed(t *testing.T) {
	t.Parallel()
	p := payload{Error: "Kaboom", StartedAt: time.Now()}
	assert.Equal(t, "Kaboom", p.failed())
	assert.Equal(t, payload{StartedAt: time.Now()}.failed(), "")
}

func TestPayloadCompletedIsNotFailedAfterTimeout(t *testing.T) {
	t.Parallel()
	p := payload{
		StartedAt: time.Now().Add(-recipeGenerationTimeout - time.Minute),
		Error:     "stale failure",
		Redirect:  "new-hash",
	}

	assert.Empty(t, p.failed())
}

func TestGenerationStatusProgressPreservesStartAndRestartClearsError(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	startedAt := time.Date(2026, 8, 25, 12, 30, 0, 0, time.FixedZone("PDT", -7*60*60))
	statuses.now = func() time.Time { return startedAt }

	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle", ""))
	require.NoError(t, statuses.Update(t.Context(), "status-lifecycle", "Gathering ingredients"))

	got, err := statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "Gathering ingredients", got.Message)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)
	assert.Empty(t, got.Error)

	require.NoError(t, statuses.Fail(t.Context(), "status-lifecycle", errors.New("store returned 404")))
	got, err = statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Equal(t, "store returned 404", got.Error)
	assert.Equal(t, startedAt.UTC(), got.StartedAt)

	retriedAt := startedAt.Add(time.Minute)
	statuses.now = func() time.Time { return retriedAt }
	require.NoError(t, statuses.Start(t.Context(), "status-lifecycle", ""))
	got, err = statuses.load(t.Context(), "status-lifecycle")
	require.NoError(t, err)
	assert.Empty(t, got.Error)
	assert.Empty(t, got.Message)
	assert.Equal(t, retriedAt.UTC(), got.StartedAt)
}

func TestUpdateKeepsFiveRecentLines(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash, ""))

	require.NoError(t, statuses.Update(t.Context(), hash, "one\ntwo\n"))
	require.NoError(t, statuses.Update(t.Context(), hash, "three\nfour\nfive"))

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "three\nfour\nfive\none\ntwo", got.Message)

	require.NoError(t, statuses.Update(t.Context(), hash, "six\nseven"))
	got, err = statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "six\nseven\nthree\nfour\nfive", got.Message)
}

func TestUpdateCapsFirstStatusAtFiveLines(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-tail"
	require.NoError(t, statuses.Start(t.Context(), hash, ""))

	require.NoError(t, statuses.Update(t.Context(), hash, "one\ntwo\nthree\nfour\nfive\nsix"))

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\nfour\nfive", got.Message)
}

func TestUpdateKeepsConcurrentLines(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	hash := "status-concurrent"
	require.NoError(t, statuses.Start(t.Context(), hash, ""))

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, status := range []string{"one", "two", "three"} {
		wg.Add(1)
		go func(status string) {
			defer wg.Done()
			errs <- statuses.Update(t.Context(), hash, status)
		}(status)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	got, err := statuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"one", "two", "three"}, strings.Split(got.Message, "\n"))
}

func TestGenerationStatusFailRecordsTerminalError(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "failed", ""))

	require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

	got, err := statuses.Load(t.Context(), "failed")
	require.NoError(t, err)
	assert.Equal(t, "plan exploded", got.Failed)
	assert.Empty(t, got.Message)
}

func TestGenerationStatusTerminalStatesAreExclusive(t *testing.T) {
	t.Parallel()
	t.Run("completed generation cannot fail", func(t *testing.T) {
		statuses := NewStore(cache.NewInMemoryCache())
		require.NoError(t, statuses.Start(t.Context(), "completed", ""))
		require.NoError(t, statuses.Complete(t.Context(), "completed", "new-hash"))

		err := statuses.Fail(t.Context(), "completed", errors.New("late failure"))
		require.ErrorContains(t, err, "already completed")

		got, loadErr := statuses.Load(t.Context(), "completed")
		require.NoError(t, loadErr)
		assert.Equal(t, "new-hash", got.Redirect)
		assert.Empty(t, got.Failed)
	})

	t.Run("failed generation cannot complete", func(t *testing.T) {
		statuses := NewStore(cache.NewInMemoryCache())
		require.NoError(t, statuses.Start(t.Context(), "failed", ""))
		require.NoError(t, statuses.Fail(t.Context(), "failed", errors.New("plan exploded")))

		err := statuses.Complete(t.Context(), "failed", "new-hash")
		require.ErrorContains(t, err, "already failed")

		got, loadErr := statuses.Load(t.Context(), "failed")
		require.NoError(t, loadErr)
		assert.Empty(t, got.Redirect)
		assert.Equal(t, "plan exploded", got.Failed)
	})
}

func TestGenerationStatusCompleteRequiresHash(t *testing.T) {
	t.Parallel()
	statuses := NewStore(cache.NewInMemoryCache())
	require.NoError(t, statuses.Start(t.Context(), "running", ""))

	err := statuses.Complete(t.Context(), "running", "  ")
	require.ErrorContains(t, err, "completed generation hash is required")
}

func TestRecipeProgressConcurrentCompletionAndFailure(t *testing.T) {
	t.Parallel()
	store := NewStore(cache.NewInMemoryCache())
	const hash = "progress"
	require.NoError(t, store.Start(t.Context(), hash, ""))
	plans := make([]ai.RecipePlan, 12)
	for i := range plans {
		plans[i].Cuisine = fmt.Sprintf("Cuisine %d", i)
	}
	require.NoError(t, store.Plan(t.Context(), hash, plans))
	var wg sync.WaitGroup
	for i := range plans {
		wg.Go(func() { assert.NoError(t, store.RecipeReady(t.Context(), hash, i, fmt.Sprintf("recipe-%d", i))) })
	}
	wg.Go(func() { assert.NoError(t, store.Update(t.Context(), hash, "Reviewing recipes")) })
	wg.Wait()
	got, err := store.Load(t.Context(), hash)
	require.NoError(t, err)
	require.Len(t, got.Slots, len(plans))
	for i, slot := range got.Slots {
		assert.Equal(t, plans[i], slot.Plan)
		assert.Equal(t, fmt.Sprintf("recipe-%d", i), slot.RecipeHash)
	}
	require.NoError(t, store.Fail(t.Context(), hash, errors.New("failed")))
	require.NoError(t, store.Update(t.Context(), hash, "Late message"))
	require.Error(t, store.RecipeReady(t.Context(), hash, 0, "late-recipe"))
	got, err = store.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.Failed)
	assert.Equal(t, "recipe-0", got.Slots[0].RecipeHash)
	require.NoError(t, store.Start(t.Context(), hash, ""))
	got, err = store.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Empty(t, got.Slots)
}

func TestRecipeProgressTimeoutRetainsReadyRecipes(t *testing.T) {
	t.Parallel()
	store := NewStore(cache.NewInMemoryCache())
	require.NoError(t, store.save(t.Context(), "timeout", payload{
		StartedAt: time.Now().Add(-11 * time.Minute),
		Slots:     []Slot{{RecipeHash: "ready"}},
	}))
	got, err := store.Load(t.Context(), "timeout")
	require.NoError(t, err)
	assert.Equal(t, "Recipe generation timed out.", got.Failed)
	assert.Equal(t, "ready", got.Slots[0].RecipeHash)
	require.Error(t, store.RecipeReady(t.Context(), "timeout", 0, "late"))
}
