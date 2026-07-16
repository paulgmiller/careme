package recipes

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	recipestatus "careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sequenceStaplesService struct {
	errs        []error
	ingredients []ai.InputIngredient
	calls       int
}

func (s *sequenceStaplesService) FetchStaples(context.Context, *GeneratorParams) ([]ai.InputIngredient, error) {
	call := s.calls
	s.calls++
	if call < len(s.errs) && s.errs[call] != nil {
		return nil, s.errs[call]
	}
	return slices.Clone(s.ingredients), nil
}

func (*sequenceStaplesService) FetchWines(context.Context, string, []string, time.Time) ([]ai.InputIngredient, error) {
	panic("unexpected call to FetchWines")
}

func testStaplesRetryPolicy(waits *[]time.Duration) staplesRetryPolicy {
	return staplesRetryPolicy{
		budget: time.Minute,
		delays: []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second},
		wait: func(_ context.Context, delay time.Duration) error {
			*waits = append(*waits, delay)
			return nil
		},
	}
}

func TestDefaultStaplesRetryPolicy(t *testing.T) {
	policy := defaultStaplesRetryPolicy()

	assert.Equal(t, 5*time.Minute, policy.budget)
	assert.Equal(t, []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second}, policy.delays)
}

func TestFetchStaples_RetriesAndEventuallySucceeds(t *testing.T) {
	staples := &sequenceStaplesService{
		errs: []error{
			errors.New("provider timeout"),
			errors.New("ingredient grade returned unknown product_id"),
			nil,
		},
		ingredients: []ai.InputIngredient{{ProductID: "apple-1", Description: "Apple"}},
	}
	statuses := &statusCounter{}
	g := &generatorService{staples: staples, statusWriter: statuses}
	var waits []time.Duration
	g.staplesRetry = testStaplesRetryPolicy(&waits)
	params := DefaultParams(&locations.Location{ID: "wholefoods_10260", Name: "Whole Foods"}, time.Now())

	got, err := g.fetchStaples(t.Context(), params.Hash(), params)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "apple-1", got[0].ProductID)
	assert.Equal(t, 3, staples.calls)
	assert.Equal(t, []time.Duration{10 * time.Second, 30 * time.Second}, waits)
	assert.Equal(t, []string{recipestatus.StaplesRetrying()}, statuses.status)
}

func TestFetchStaples_ExhaustionReturnsTypedError(t *testing.T) {
	lastErr := errors.New("final grading failure")
	staples := &sequenceStaplesService{errs: []error{
		errors.New("first failure"),
		errors.New("second failure"),
		errors.New("third failure"),
		lastErr,
	}}
	g := &generatorService{staples: staples, statusWriter: noopstatuswriter{}}
	var waits []time.Duration
	g.staplesRetry = testStaplesRetryPolicy(&waits)
	params := DefaultParams(&locations.Location{ID: "wholefoods_10260", Name: "Whole Foods"}, time.Now())

	_, err := g.fetchStaples(t.Context(), params.Hash(), params)

	var unavailable *staplesUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, 4, unavailable.attempts)
	assert.ErrorIs(t, err, lastErr)
	assert.Equal(t, 4, staples.calls)
	assert.Equal(t, []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second}, waits)
}

func TestFetchStaples_StopsWhenParentContextIsCanceled(t *testing.T) {
	staples := &sequenceStaplesService{errs: []error{errors.New("provider failure")}}
	g := &generatorService{staples: staples, statusWriter: noopstatuswriter{}}
	var waits []time.Duration
	g.staplesRetry = testStaplesRetryPolicy(&waits)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	params := DefaultParams(&locations.Location{ID: "wholefoods_10260", Name: "Whole Foods"}, time.Now())

	_, err := g.fetchStaples(ctx, params.Hash(), params)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, staples.calls)
	assert.Empty(t, waits)
}

func TestWaitForStaplesRetry_StopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := waitForStaplesRetry(ctx, time.Hour)

	assert.ErrorIs(t, err, context.Canceled)
}
