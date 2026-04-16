package recipes

import (
	"context"
	"testing"

	"careme/internal/ai"
)

type recipeSaver interface {
	saveRecipes(ctx context.Context, recipes []ai.Recipe) error
}

func saveRecipesForOrigin(t *testing.T, saver recipeSaver, originHash string, recipes ...ai.Recipe) {
	t.Helper()

	for i := range recipes {
		recipes[i].OriginHash = originHash
	}

	if err := saver.saveRecipes(t.Context(), recipes); err != nil {
		t.Fatalf("failed to save recipes: %v", err)
	}
}
