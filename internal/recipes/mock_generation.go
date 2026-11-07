package recipes

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/google/uuid"
)

// mockMenuClient uses the same slot coordinator as live generation.
type mockMenuClient struct{ mock mock }

var _ aiClient = mockMenuClient{}

func mockGenerationDelay(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m mockMenuClient) CreateMenuPlan(context.Context, *locations.Location, []ai.InputIngredient, []string, time.Time, []string, int) (*ai.MenuPlan, error) {
	return nil, fmt.Errorf("mock initial menu is created by GenerateRecipes")
}

func (m mockMenuClient) RegenerateMenuPlan(ctx context.Context, instructions []string, _ ai.ResponseRef, count int) (*ai.MenuPlan, error) {
	if err := mockGenerationDelay(ctx); err != nil {
		return nil, err
	}
	text := strings.ToLower(strings.Join(instructions, "\n"))
	menu := &ai.MenuPlan{ResponseID: uuid.NewString()}
	for _, index := range rand.Perm(len(mockRecipes)) {
		recipe := mockRecipes[index]
		if strings.Contains(text, strings.ToLower(recipe.Title)) {
			continue
		}
		menu.Plans = append(menu.Plans, ai.RecipePlan{Cuisine: recipe.Title, AnchorIngredient: recipe.Ingredients[0].Name, Category: ai.MealCategoryDifferent})
		if len(menu.Plans) == count {
			return menu, nil
		}
	}
	return nil, fmt.Errorf("mock replacement menu: insufficient unused meal ideas for %d meals", count)
}

func (m mockMenuClient) GenerateRecipe(ctx context.Context, instructions []string, _ ai.ResponseRef) (*ai.Recipe, error) {
	if err := mockGenerationDelay(ctx); err != nil {
		return nil, err
	}
	for _, instruction := range instructions {
		for _, recipe := range mockRecipes {
			if instruction == fmt.Sprintf("Cuisine direction for this recipe: %s.", recipe.Title) {
				recipe.ResponseID = uuid.NewString()
				return &recipe, nil
			}
		}
	}
	return nil, fmt.Errorf("mock recipe generation: no recipe matches the meal plan")
}

func (m mockMenuClient) Regenerate(ctx context.Context, instructions []string, previous ai.ResponseRef) (*ai.Recipe, error) {
	return m.mock.RegenerateRecipe(ctx, instructions, previous)
}

func (m mockMenuClient) AskQuestion(ctx context.Context, question string, previous ai.ResponseRef) (*ai.QuestionResponse, error) {
	return m.mock.AskQuestion(ctx, question, previous)
}

func (m mockMenuClient) PickWine(ctx context.Context, recipe ai.Recipe, _ []ai.InputIngredient) (*ai.WineSelection, error) {
	return m.mock.PickAWine(ctx, "", recipe, time.Time{})
}

type mockGenerationCritiquer struct{ critiquer mockRecipeCritiquer }

func (m mockGenerationCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	return m.critiquer.CritiqueRecipe(ctx, recipe)
}

func (mockGenerationCritiquer) CritiqueRecipeInBackground(context.Context, ai.Recipe) {}
