package recipes

import (
	"context"
	"testing"
	"time"

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

func (f recipeImageStarterFunc) RecipeReady(ctx context.Context, recipe ai.Recipe, locationID string, date time.Time) error {
	f(ctx, recipe)
	return nil
}

type noopRecipeImages struct{}

func (noopRecipeImages) RecipeReady(context.Context, ai.Recipe, string, time.Time) error { return nil }

type (
	winePicker interface {
		pickAWine(context.Context, string, ai.Recipe, time.Time) (*ai.WineSelection, error)
	}
	testWineInputs struct {
		location string
		date     time.Time
	}
	testWineAI struct {
		aiClient
		picker winePicker
		inputs *testWineInputs
	}
)

func (w testWineAI) PickWine(ctx context.Context, recipe ai.Recipe, _ []ai.InputIngredient) (*ai.WineSelection, error) {
	return w.picker.pickAWine(ctx, w.inputs.location, recipe, w.inputs.date)
}

type testWineStaples struct {
	staplesService
	inputs *testWineInputs
}

func (w testWineStaples) FetchWines(_ context.Context, location string, _ []string, date time.Time) ([]ai.InputIngredient, error) {
	w.inputs.location = location
	w.inputs.date = date
	return []ai.InputIngredient{{ProductID: "wine"}}, nil
}

func testWineWorker(rio recipeio, picker winePicker) recipeWineGenerator {
	inputs := &testWineInputs{}
	return recipeWineGenerator{rio, testWineAI{picker: picker, inputs: inputs}, testWineStaples{inputs: inputs}}
}
