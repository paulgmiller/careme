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

func (c *tracingAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previous ai.ResponseRef, count int) (*ai.MenuPlan, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.regenerate_menu_plan",
		trace.WithAttributes(attribute.Int("recipe_plan.count", count)),
	)
	defer span.End()

	return c.next.RegenerateMenuPlan(ctx, instructions, previous, count)
}

func (c *tracingAIClient) GenerateRecipe(ctx context.Context, instructions []string, menu ai.ResponseRef) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.generate_recipe")
	defer span.End()

	return c.next.GenerateRecipe(ctx, instructions, menu)
}

func (c *tracingAIClient) Regenerate(ctx context.Context, newinstructions []string, previous ai.ResponseRef) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.regenerate")
	defer span.End()

	return c.next.Regenerate(ctx, newinstructions, previous)
}

func (c *tracingAIClient) AskQuestion(ctx context.Context, question string, previous ai.ResponseRef) (*ai.QuestionResponse, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.ask_question")
	defer span.End()

	return c.next.AskQuestion(ctx, question, previous)
}

func (c *tracingAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	ctx, span := tracer.Start(ctx, "recipes.ai.pick_wine")
	defer span.End()

	return c.next.PickWine(ctx, recipe, wines)
}
