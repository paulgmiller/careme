package critique

import (
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
)

func TestMultiCritiquerCritiquesEachRecipe(t *testing.T) {
	t.Parallel()

	base := &stubCritiquer{
		critique: &ai.RecipeCritique{
			SchemaVersion: "recipe-critique-v1",
			OverallScore:  8,
			Summary:       "Solid.",
		},
	}
	mc := NewMultiCritiquer(base)
	recipes := []ai.Recipe{
		{Title: "One"},
		{Title: "Two"},
	}

	results := mc.CritiqueRecipes(t.Context(), recipes)

	var got []Result
	for result := range results {
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

func TestNewServiceReturnsRubberstampWithoutGemini(t *testing.T) {
	t.Parallel()

	svc := NewManager(&config.Config{}, cache.NewFileCache(t.TempDir()))

	results := svc.CritiqueRecipes(t.Context(), []ai.Recipe{{Title: "Weeknight Pasta"}})
	result, ok := <-results
	if !ok {
		t.Fatal("expected critique result")
	}

	if result.Critique == nil || result.Critique.OverallScore != 10 {
		t.Fatalf("unexpected rubberstamp critique: %#v", result.Critique)
	}
}
