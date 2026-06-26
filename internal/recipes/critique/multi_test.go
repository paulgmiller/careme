package critique

import (
	"testing"

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
