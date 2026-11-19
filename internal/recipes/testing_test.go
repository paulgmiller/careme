package recipes

import (
	"context"
	"testing"

	"careme/internal/ai"
)

func saveRecipesForOrigin(t *testing.T, saver recipeSaver, originHash string, recipes ...ai.Recipe) {
	t.Helper()

	for i := range recipes {
		recipes[i].OriginHash = originHash
	}

	for _, r := range recipes {
		if err := saver.SaveRecipe(t.Context(), r); err != nil {
			t.Fatalf("failed to save recipes: %v", err)
		}
	}
}

type recipeImageStarterFunc func(context.Context, ai.Recipe)

func (f recipeImageStarterFunc) GenerateInBackground(ctx context.Context, recipe ai.Recipe) {
	f(ctx, recipe)
}

type noopRecipeImages struct{}

func (noopRecipeImages) GenerateInBackground(context.Context, ai.Recipe) {}
