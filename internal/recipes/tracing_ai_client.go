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
) (*ai.MenuPlan, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.create_menu_plan")
	defer span.End()

	return c.next.CreateMenuPlan(ctx, location, ingredients, instructions, date, lastRecipes)
}

func (c *tracingAIClient) GenerateRecipe(
	ctx context.Context,
	location *locations.Location,
	ingredients []ai.InputIngredient,
	instructions []string,
	date time.Time,
	lastRecipes []string,
	plan ai.RecipePlan,
) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.generate_recipe",
		trace.WithAttributes(attribute.Bool("recipe_plan.fancy", plan.Fancy)),
	)
	defer span.End()

	return c.next.GenerateRecipe(ctx, location, ingredients, instructions, date, lastRecipes, plan)
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
