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
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type aiClient interface {
	CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.MenuPlan, error)
	GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, plan ai.RecipePlan) (*ai.Recipe, error)
	Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error)
	AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error)
	PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error)
}

type staplesService interface {
	FetchStaples(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error)
	// only used for wine. Probably need a refactoro
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int, date time.Time) ([]ai.InputIngredient, error)
}

type recipeSaver interface {
	SaveRecipe(ctx context.Context, recipes ai.Recipe) error
}

type generatorService struct {
	aiClient     aiClient
	critiquer    critique.Service
	staples      staplesService
	statusWriter statusWriter
	saver        recipeSaver
}

var tracer = otel.Tracer("careme/internal/recipes")

func NewGenerator(aiClient aiClient, critiquer critique.Service, staples staplesService, statuses statusWriter, recipeSaver recipeSaver) (*generatorService, error) {
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
		aiClient:     aiClient,
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

	if wholefoods.NewIdentityProvider().IsID(location) {
		styles = []string{"red-wine", "white-wine", "sparkling"}
	}

	if len(styles) == 0 {
		return &ai.WineSelection{Commentary: "no wines styles for recipe", Wines: []ai.Ingredient{}}, nil
	}

	wines, err := parallelism.Flatten(styles, func(style string) ([]ai.InputIngredient, error) {
		return g.staples.GetIngredients(ctx, location, style, 0, date)
	})
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
	return selection, nil
}

func (g *generatorService) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
	start := time.Now()

	if len(p.Dismissed) > 0 || len(p.Saved) > 0 {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "dismissed_count", len(p.Dismissed))
		ctx, span := tracer.Start(ctx, "recipes.regenerate")
		defer span.End()

		g.writeStatus(ctx, hash, status.Regen(p.Instructions, p.Dismissed))

		// Should get more menup plans here.
		// https://github.com/paulgmiller/careme/issues/570

		baseInstructions := regenerateInstructions(p)
		results, err := parallelism.MapWithErrors(p.Dismissed, func(dismissed ai.Recipe) (*ai.Recipe, error) {
			if strings.TrimSpace(dismissed.ResponseID) == "" {
				return nil, fmt.Errorf("recipe %q is missing response ID for regeneration", dismissed.Title)
			}
			slog.InfoContext(ctx, "dismissed recipe", "hash", dismissed.ComputeHash(), "title", dismissed.Title)

			instructions := append(slices.Clone(baseInstructions), "Passed on "+dismissed.Title)
			// todo add in alternate menu plan?
			recipe, err := g.aiClient.Regenerate(ctx, instructions, dismissed.ResponseID)
			if err != nil {
				return nil, err
			}
			recipe.OriginHash = hash
			recipe.ParentHash = dismissed.ComputeHash()
			return g.critiqueAndMaybeRetryRecipe(ctx, hash, recipe, nil)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}

		recipes := append(lo.FromSlicePtr(results), p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return &ai.ShoppingList{
			Recipes: recipes,
			// Plan: how do we get this>?
		}, nil
	}

	ctx, span := tracer.Start(ctx, "recipes.generate")
	defer span.End()
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.staples.FetchStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}
	ogCount := len(ingredients)
	ingredients = lo.Filter(ingredients, func(ing ai.InputIngredient, _ int) bool {
		// TODO make configurable?
		return ing.Grade == nil || ing.Grade.Score > 6
	})

	g.writeStatus(ctx, hash, status.Ingredients(ingredients, ogCount))
	mutable.Shuffle(ingredients)

	instructions := []string{p.Directive, p.Instructions}

	// pass in count?
	menuPlan, err := g.aiClient.CreateMenuPlan(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to plan recipe variety: %w", err)
	}
	// do we need to save menuplan outside of shoppinglist?
	planCount := min(3, len(menuPlan.Plans))
	recipePlans, leftovers := menuPlan.Plans[:planCount], menuPlan.Plans[planCount:]

	results, err := parallelism.MapWithErrors(recipePlans, func(plan ai.RecipePlan) (*ai.Recipe, error) {
		recipe, err := g.aiClient.GenerateRecipe(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes, plan)
		if err != nil {
			return nil, err
		}
		enrichRecipeIngredientMetadata(recipe, ingredients)
		// would prefer to do this deeper down in client like response id but have to pass in the hash
		recipe.OriginHash = hash
		return g.critiqueAndMaybeRetryRecipe(ctx, hash, recipe, ingredients)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return &ai.ShoppingList{
		Recipes: lo.FromSlicePtr(results),
		Plan:    &ai.MenuPlan{Plans: leftovers, Notes: menuPlan.Notes},
	}, nil
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
	return instructions
}

func (g *generatorService) critiqueAndMaybeRetryRecipe(ctx context.Context, hash string, recipe *ai.Recipe, inputs []ai.InputIngredient) (*ai.Recipe, error) {
	if err := g.saver.SaveRecipe(ctx, *recipe); err != nil {
		return nil, err
	}
	if g.critiquer == nil {
		return recipe, nil
	}
	ctx, span := tracer.Start(ctx, "recipes.critique.recipe")
	defer span.End()

	// going to overwrite other statuss
	g.writeStatus(ctx, hash, status.Titles("Getting feedback on: ", []ai.Recipe{*recipe}))

	result := <-g.critiquer.CritiqueRecipe(ctx, *recipe)
	if result.Err != nil {
		slog.ErrorContext(ctx, "failed to critique recipe", "hash", hash, "title", recipe.Title, "error", result.Err)
		return recipe, nil
	}
	if result.Critique == nil || result.Critique.OverallScore >= critique.MinimumRecipeScore {
		return recipe, nil
	}

	span.SetAttributes(attribute.Bool("regenaftercrique", true))
	slog.InfoContext(ctx, "low scoring recipe", "hash", hash, "title", recipe.Title, "score", result.Critique.OverallScore)
	// going to overwrite other statuses
	g.writeStatus(ctx, hash, status.Titles("Making adjustments to this recipe: ", []ai.Recipe{*recipe}))

	// panic?
	if strings.TrimSpace(recipe.ResponseID) == "" {
		return nil, fmt.Errorf("recipe %q is missing response ID for critique retry", recipe.Title)
	}
	retry, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions(result), recipe.ResponseID)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipe %q from critique feedback: %w", recipe.Title, err)
	}
	enrichRecipeIngredientMetadata(retry, inputs)
	retry.OriginHash = hash
	retry.ParentHash = recipe.ComputeHash()
	if err := g.saver.SaveRecipe(ctx, *retry); err != nil {
		return nil, err
	}
	// don't block
	g.critiqueInBackground(ctx, *retry)

	return retry, nil
}

func (g *generatorService) critiqueInBackground(ctx context.Context, recipe ai.Recipe) {
	results := g.critiquer.CritiqueRecipe(ctx, recipe)
	go func() {
		for result := range results {
			if result.Err != nil {
				slog.ErrorContext(ctx, "failed to critique retried recipe", "hash", recipe.ComputeHash(), "title", recipe.Title, "error", result.Err)
			}
		}
	}()
}

func enrichRecipeIngredientMetadata(recipe *ai.Recipe, inputs []ai.InputIngredient) {
	if recipe == nil || len(inputs) == 0 {
		return
	}

	byProductID := make(map[string]ai.InputIngredient, len(inputs))
	for _, input := range inputs {
		productID := strings.TrimSpace(input.ProductID)
		if productID == "" {
			continue
		}
		byProductID[productID] = input
	}

	for i := range recipe.Ingredients {
		ingredient := &recipe.Ingredients[i]
		input, ok := byProductID[strings.TrimSpace(ingredient.ProductID)]
		if !ok {
			continue
		}
		ingredient.ProductID = strings.TrimSpace(input.ProductID)
		ingredient.AisleNumber = strings.TrimSpace(input.AisleNumber)
		if price := inputIngredientDisplayPrice(input); price != "" {
			ingredient.Price = price
		}
	}
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
