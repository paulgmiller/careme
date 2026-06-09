package recipes

import (
	"context"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type tracingAIClient struct {
	next aiClient
}

func (c *tracingAIClient) CreateMenuPlan(
	ctx context.Context,
	location *locations.Location,
	ingredients []ai.InputIngredient,
	instructions []string,
	date time.Time,
	lastRecipes []string,
	count int,
) (*ai.MenuPlan, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.create_menu_plan")
	defer span.End()

	return c.next.CreateMenuPlan(ctx, location, ingredients, instructions, date, lastRecipes, count)
}

func (c *tracingAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.regenerate_menu_plan",
		trace.WithAttributes(attribute.Int("recipe_plan.count", count)),
	)
	defer span.End()

	return c.next.RegenerateMenuPlan(ctx, instructions, previousResponseID, count)
}

func (c *tracingAIClient) PrepareRecipeContext(
	ctx context.Context,
	location *locations.Location,
	ingredients []ai.InputIngredient,
	instructions []string,
	date time.Time,
	lastRecipes []string,
) (*ai.RecipeContext, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.prepare_recipe_context")
	defer span.End()

	return c.next.PrepareRecipeContext(ctx, location, ingredients, instructions, date, lastRecipes)
}

func (c *tracingAIClient) GenerateRecipeFromContext(ctx context.Context, instructions []string, recipeContext ai.RecipeContext) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.generate_recipe_from_context")
	defer span.End()

	return c.next.GenerateRecipeFromContext(ctx, instructions, recipeContext)
}

func (c *tracingAIClient) Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.regenerate")
	defer span.End()

	return c.next.Regenerate(ctx, newinstructions, previousResponseID)
}

func (c *tracingAIClient) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.ask_question")
	defer span.End()

	return c.next.AskQuestion(ctx, question, previousResponseID)
}

func (c *tracingAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.pick_wine")
	defer span.End()

	return c.next.PickWine(ctx, recipe, wines)
}
