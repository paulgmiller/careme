package critique

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
)

type stubCritiquer struct {
	readyErr error
	calls    int
	critique *ai.RecipeCritique
	err      error
}

func (s *stubCritiquer) Ready(context.Context) error {
	return s.readyErr
}

func (s *stubCritiquer) CritiqueRecipe(_ context.Context, _ ai.Recipe) (*ai.RecipeCritique, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.critique, nil
}

func TestCachingCritiquerUsesCacheOnSecondCall(t *testing.T) {
	t.Parallel()

	base := &stubCritiquer{
		critique: &ai.RecipeCritique{
			SchemaVersion: "recipe-critique-v1",
			OverallScore:  9,
			Summary:       "Great.",
		},
	}
	critiquer := newCachingCritiquer(base, NewStore(cache.NewFileCache(t.TempDir())))
	recipe := ai.Recipe{Title: "Roast Chicken"}

	first, err := critiquer.CritiqueRecipe(t.Context(), recipe)
	if err != nil {
		t.Fatalf("first CritiqueRecipe failed: %v", err)
	}
	second, err := critiquer.CritiqueRecipe(t.Context(), recipe)
	if err != nil {
		t.Fatalf("second CritiqueRecipe failed: %v", err)
	}

	if base.calls != 1 {
		t.Fatalf("calls = %d, want 1", base.calls)
	}
	if first.Summary != second.Summary {
		t.Fatalf("summary mismatch: first=%q second=%q", first.Summary, second.Summary)
	}
}

func TestCachingCritiquerReadyDelegates(t *testing.T) {
	t.Parallel()

	want := errors.New("not ready")
	critiquer := newCachingCritiquer(&stubCritiquer{readyErr: want}, NewStore(cache.NewFileCache(t.TempDir())))

	if err := critiquer.Ready(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Ready error = %v, want %v", err, want)
	}
}
