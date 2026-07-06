package recipes

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/status"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const IngredientGradeCutoff = 6

type aiClient interface {
	CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error)
	RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error)
	GenerateRecipe(ctx context.Context, instructions []string, menuResponseID string, searchableIngredients []ai.InputIngredient) (*ai.Recipe, error)
	Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error)
	AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error)
	PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error)
}

type staplesService interface {
	FetchStaples(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error)
	FetchWines(ctx context.Context, locationID string, styles []string, date time.Time) ([]ai.InputIngredient, error)
}

type recipeSaver interface {
	SaveRecipe(ctx context.Context, recipes ai.Recipe) error
}

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
	CritiqueRecipeInBackground(ctx context.Context, recipe ai.Recipe)
}

type generatorService struct {
	aiClient     aiClient
	critiquer    recipeCritiquer
	staples      staplesService
	statusWriter statusWriter
	saver        recipeSaver
}

var tracer = otel.Tracer("careme/internal/recipes")

func NewGenerator(aiClient aiClient, critiquer recipeCritiquer, staples staplesService, statuses statusWriter, recipeSaver recipeSaver) (*generatorService, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is required")
	}
	if critiquer == nil {
		return nil, fmt.Errorf("critiquer is required")
	}
	if staples == nil {
		return nil, fmt.Errorf("staples service is required")
	}
	if recipeSaver == nil {
		return nil, fmt.Errorf("recipe saver is required")
	}
	return &generatorService{
		aiClient:     &tracingAIClient{aiClient},
		critiquer:    critiquer,
		staples:      staples,
		statusWriter: statuses,
		saver:        recipeSaver,
	}, nil
}

func (g *generatorService) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	ctx, span := tracer.Start(ctx, "recipes.pickawine")
	defer span.End()
	var styles []string
	for _, style := range recipe.WineStyles {
		style = strings.TrimSpace(style)
		if style != "" {
			styles = append(styles, style)
		}
	}

	if len(styles) == 0 {
		return &ai.WineSelection{Commentary: "no wines styles for recipe", Wines: []ai.Ingredient{}}, nil
	}

	wines, err := g.staples.FetchWines(ctx, location, styles, date)
	if err != nil {
		return nil, err
	}

	if len(wines) == 0 {
		return &ai.WineSelection{Commentary: "no wines found", Wines: []ai.Ingredient{}}, nil
	}
	wines = lo.UniqBy(wines, func(i ai.InputIngredient) string {
		return i.ProductID
	})

	selection, err := g.aiClient.PickWine(ctx, recipe, wines)
	if err != nil {
		return nil, err
	}
	enrichIngredientsMetadata(selection.Wines, inputIngredientMap(wines))
	return selection, nil
}

func (g *generatorService) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
	start := time.Now()

	if p.isRegeneration() {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "dismissed_count", len(p.Dismissed))
		ctx, span := tracer.Start(ctx, "recipes.regenerate")
		defer span.End()

		g.writeStatus(ctx, hash, status.Regen(p.Instructions, p.Dismissed))

		regenInstructions := regenerateInstructions(p)

		// this SHOULD hit the cache and we could do it in parallel with menu planning
		allIngredients, err := g.staples.FetchStaples(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("failed to get staples: %w", err)
		}
		ingMap := inputIngredientMap(allIngredients)
		replacmentCount := max(len(p.Dismissed), 1) // if no dismissed then just regenerate one and hope for better, if dismissed then regenerate all dismissed
		plan, err := g.replacementMenuPlan(ctx, p, regenInstructions, replacmentCount)
		if err != nil {
			return nil, fmt.Errorf("failed to plan recipe replacements: %w", err)
		}
		g.writeStatus(ctx, hash, plan.String())
		menuResponseID := strings.TrimSpace(plan.ResponseID)

		results, err := parallelism.MapWithErrors(plan.Plans, func(plan ai.RecipePlan) (*ai.Recipe, error) {
			ctx, span := tracer.Start(ctx, "recipes.regenerate.single")
			defer span.End()

			recipe, err := g.aiClient.GenerateRecipe(ctx, plan.Instructions(), menuResponseID, allIngredients)
			if err != nil {
				return nil, err
			}
			recipe.OriginHash = hash
			enrichIngredientsMetadata(recipe.Ingredients, ingMap)
			if err := g.saver.SaveRecipe(ctx, *recipe); err != nil {
				return nil, err
			}
			return g.critiqueAndMaybeRetryRecipe(ctx, hash, recipe, ingMap)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to generate replacement recipes with AI: %w", err)
		}

		recipes := append(lo.FromSlicePtr(results), p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return &ai.ShoppingList{
			Recipes: recipes,
			Plan:    plan, // should we append to last plan? only saved ones?
		}, nil
	}

	ctx, span := tracer.Start(ctx, "recipes.generate")
	defer span.End()
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())

	allIngredients, err := g.staples.FetchStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}
	ogCount := len(allIngredients)
	menuIngredients := lo.Filter(allIngredients, func(ing ai.InputIngredient, _ int) bool {
		// TODO make configurable?
		return ing.Grade == nil || ing.Grade.Score > IngredientGradeCutoff
	})
	ingMap := inputIngredientMap(allIngredients)

	g.writeStatus(ctx, hash, status.Ingredients(menuIngredients, ogCount))
	mutable.Shuffle(menuIngredients)

	menuPlanInstructions := []string{p.Directive, p.Instructions}

	menuPlan, err := g.aiClient.CreateMenuPlan(ctx, p.Location, menuIngredients, menuPlanInstructions, p.Date, p.LastRecipes, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to plan recipe variety: %w", err)
	}
	menuResponseID := strings.TrimSpace(menuPlan.ResponseID)

	g.writeStatus(ctx, hash, menuPlan.String())

	results, err := parallelism.MapWithErrors(menuPlan.Plans, func(plan ai.RecipePlan) (*ai.Recipe, error) {
		ctx, span := tracer.Start(ctx, "recipes.generate.single")
		defer span.End()
		recipeInstructions := append([]string{p.Directive}, plan.Instructions()...)
		recipe, err := g.aiClient.GenerateRecipe(ctx, recipeInstructions, menuResponseID, allIngredients)
		if err != nil {
			return nil, err
		}
		// would prefer to do this deeper down in client like response id but have to pass in the hash
		recipe.OriginHash = hash

		enrichIngredientsMetadata(recipe.Ingredients, ingMap)
		if err := g.saver.SaveRecipe(ctx, *recipe); err != nil {
			return nil, err
		}
		return g.critiqueAndMaybeRetryRecipe(ctx, hash, recipe, ingMap)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return &ai.ShoppingList{
		Recipes: lo.FromSlicePtr(results),
		Plan:    menuPlan,
	}, nil
}

func (p *generatorParams) isRegeneration() bool {
	return len(p.Dismissed) > 0 || len(p.Saved) > 0 || strings.TrimSpace(p.PreviousMenuPlanResponseID) != ""
}

func (g *generatorService) replacementMenuPlan(ctx context.Context, p *generatorParams, instructions []string, count int) (*ai.MenuPlan, error) {
	if strings.TrimSpace(p.PreviousMenuPlanResponseID) == "" {
		return nil, fmt.Errorf("missing previous menu plan response ID for menu")
	}
	plan, err := g.aiClient.RegenerateMenuPlan(ctx, instructions, p.PreviousMenuPlanResponseID, count)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("AI returned no menu plan")
	}
	if len(plan.Plans) == 0 {
		return nil, fmt.Errorf("planned 0 replacement recipes")
	}
	if strings.TrimSpace(plan.ResponseID) == "" {
		return nil, fmt.Errorf("failed to plan recipe replacements: AI returned no menu plan response ID")
	}
	return plan, nil
}

func (g *generatorService) RegenerateRecipe(ctx context.Context, instructions []string, previousResponseID string) (*ai.Recipe, error) {
	r, err := g.aiClient.Regenerate(ctx, instructions, previousResponseID)
	if err != nil {
		return nil, err
	}
	// don't block
	g.critiquer.CritiqueRecipeInBackground(ctx, *r)
	return r, nil
}

// generator not prociding a lot of value here. Should sever just hold an ai client?
func (g *generatorService) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	return g.aiClient.AskQuestion(ctx, question, previousResponseID)
}

func newlySaved(saved []ai.Recipe, priorSavedHashes []string) []string {
	titles := make([]string, 0, len(saved))
	for _, recipe := range saved {
		hash := recipe.ComputeHash()
		if slices.Contains(priorSavedHashes, hash) {
			continue
		}
		titles = append(titles, recipe.Title)
	}
	return lo.Uniq(titles)
}

func regenerateInstructions(p *generatorParams) []string {
	instructions := make([]string, 0, 1+len(p.Dismissed)+len(p.Saved))
	if trimmed := strings.TrimSpace(p.Instructions); trimmed != "" {
		instructions = append(instructions, trimmed)
	}
	for _, saved := range newlySaved(p.Saved, p.PriorSavedHashes) {
		instructions = append(instructions, "Enjoyed and saved so don't repeat: "+saved)
	}
	for _, recipe := range p.Dismissed {
		if title := strings.TrimSpace(recipe.Title); title != "" {
			instructions = append(instructions, "Passed on "+title)
		}
	}
	return instructions
}

func (g *generatorService) critiqueAndMaybeRetryRecipe(ctx context.Context, hash string, recipe *ai.Recipe, ingMap map[string]ai.InputIngredient) (*ai.Recipe, error) {
	ctx, span := tracer.Start(ctx, "recipes.critique.recipe")
	defer span.End()

	g.writeStatus(ctx, hash, "Getting feedback on "+recipe.Title+"\n")

	c, err := g.critiquer.CritiqueRecipe(ctx, *recipe)
	if err != nil {
		slog.ErrorContext(ctx, "failed to critique recipe", "hash", hash, "title", recipe.Title, "error", err)
		return recipe, nil
	}
	if c.OverallScore >= critique.MinimumRecipeScore {
		return recipe, nil
	}

	span.SetAttributes(attribute.Bool("regenaftercrique", true))
	slog.InfoContext(ctx, "low scoring recipe", "hash", hash, "title", recipe.Title, "score", c.OverallScore)
	// going to overwrite other statuses
	g.writeStatus(ctx, hash, "Adjusting "+recipe.Title+"\n")

	// panic?
	if strings.TrimSpace(recipe.ResponseID) == "" {
		return nil, fmt.Errorf("recipe %q is missing response ID for critique retry", recipe.Title)
	}
	retry, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions(*c), recipe.ResponseID)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipe %q from critique feedback: %w", recipe.Title, err)
	}
	enrichIngredientsMetadata(retry.Ingredients, ingMap)
	retry.OriginHash = hash
	retry.ParentHash = recipe.ComputeHash()
	if err := g.saver.SaveRecipe(ctx, *retry); err != nil {
		return nil, err
	}
	// don't block
	g.critiquer.CritiqueRecipeInBackground(ctx, *retry)

	return retry, nil
}

func enrichIngredientsMetadata(ingredients []ai.Ingredient, byProductID map[string]ai.InputIngredient) {
	for i := range ingredients {
		ingredient := &ingredients[i] // should we mutate or create new ingredient.
		input, ok := byProductID[strings.TrimSpace(ingredient.ProductID)]
		if !ok {
			continue
		}
		ingredient.ProductID = strings.TrimSpace(input.ProductID)
		ingredient.AisleNumber = strings.TrimSpace(input.AisleNumber)
		ingredient.Price = inputIngredientDisplayPrice(input)
	}
}

func inputIngredientMap(ingredients []ai.InputIngredient) map[string]ai.InputIngredient {
	return lo.SliceToMap(ingredients, func(ing ai.InputIngredient) (string, ai.InputIngredient) {
		return strings.TrimSpace(ing.ProductID), ing
	})
}

func inputIngredientDisplayPrice(input ai.InputIngredient) string {
	price := input.PriceSale
	if price == nil {
		price = input.PriceRegular
	}
	if price == nil {
		return ""
	}
	return fmt.Sprintf("$%.2f", *price)
}

// just making this best effort
func (g *generatorService) writeStatus(ctx context.Context, hash string, status string) {
	if strings.TrimSpace(hash) == "" {
		return
	}
	if err := g.statusWriter.SaveGenerationStatus(ctx, hash, status); err != nil {
		slog.ErrorContext(ctx, "failed to save generation status", "hash", hash, "status", status, "error", err)
	}
}
