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

func (f recipeImageStarterFunc) RecipeReady(ctx context.Context, recipe ai.Recipe) error {
	f(ctx, recipe)
	return nil
}

type noopRecipeImages struct{}

func (noopRecipeImages) RecipeReady(context.Context, ai.Recipe) error { return nil }
