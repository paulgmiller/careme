package critique

import (
	"context"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

func TestWaitingCritiquerCritiquesEachRecipe(t *testing.T) {
	t.Parallel()

	base := &stubCritiquer{
		critique: &ai.RecipeCritique{
			SchemaVersion: "recipe-critique-v1",
			OverallScore:  8,
			Summary:       "Solid.",
		},
	}
	mc := &waitingCritiquer{
		critiquer: base,
	}
	recipes := []ai.Recipe{
		{Title: "One"},
		{Title: "Two"},
	}

	var got []*ai.RecipeCritique
	for _, recipe := range recipes {
		result, err := mc.CritiqueRecipe(t.Context(), recipe)
		if err != nil {
			t.Fatalf("CritiqueRecipe failed: %v", err)
		}
		got = append(got, result)
	}
	mc.Wait()

	if len(got) != len(recipes) {
		t.Fatalf("results = %d, want %d", len(got), len(recipes))
	}
	if base.calls != len(recipes) {
		t.Fatalf("calls = %d, want %d", base.calls, len(recipes))
	}
}

func TestRubberStampReturnsPassingCritique(t *testing.T) {
	t.Parallel()

	svc := NewMock(cache.NewFileCache(t.TempDir()))
	result, err := svc.CritiqueRecipe(t.Context(), ai.Recipe{Title: "Weeknight Pasta"})
	if err != nil {
		t.Fatalf("CritiqueRecipe failed: %v", err)
	}

	if result == nil || result.OverallScore != 10 {
		t.Fatalf("unexpected rubberstamp critique: %#v", result)
	}
}

func TestWaitingCritiquerWaitsForBackgroundCritique(t *testing.T) {
	t.Parallel()

	base := &blockingCritiquer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	mc := &waitingCritiquer{critiquer: base}

	mc.CritiqueRecipeInBackground(t.Context(), ai.Recipe{Title: "Slow Dinner"})
	<-base.started

	waitDone := make(chan struct{})
	go func() {
		mc.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("Wait returned before background critique finished")
	case <-time.After(10 * time.Millisecond):
	}

	close(base.release)
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after background critique finished")
	}
}

type blockingCritiquer struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingCritiquer) Ready(context.Context) error {
	return nil
}

func (b *blockingCritiquer) CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error) {
	close(b.started)
	<-b.release
	return &ai.RecipeCritique{OverallScore: 10}, nil
}
