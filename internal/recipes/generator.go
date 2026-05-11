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
	CreateMenuPlan(ctx context.Context, ingredients []ai.InputIngredient, date time.Time, location *locations.Location) (*ai.MenuPlan, error)
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

type generatorService struct {
	aiClient     aiClient
	critiquer    critique.Service
	staples      staplesService
	statusWriter statusWriter
}

type recipePipelineResult struct {
	Recipe    ai.Recipe
	Discarded []ai.Recipe
}

var tracer = otel.Tracer("careme/internal/recipes")

func NewGenerator(aiClient aiClient, critiquer critique.Service, staples staplesService, statuses statusWriter) (*generatorService, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is required")
	}
	if critiquer == nil {
		return nil, fmt.Errorf("critiquer is required")
	}
	if staples == nil {
		return nil, fmt.Errorf("staples service is required")
	}
	return &generatorService{
		aiClient:     aiClient,
		critiquer:    critiquer,
		staples:      staples,
		statusWriter: statuses,
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

	if len(p.Dismissed) > 0 {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "dismissed_count", len(p.Dismissed))
		ctx, span := tracer.Start(ctx, "recipes.regenerate")
		defer span.End()

		g.writeStatus(ctx, hash, status.Regen(p.Instructions, p.Dismissed))

		baseInstructions := regenerateInstructions(p)
		results, err := parallelism.MapWithErrors(p.Dismissed, func(dismissed ai.Recipe) (recipePipelineResult, error) {
			if strings.TrimSpace(dismissed.ResponseID) == "" {
				return recipePipelineResult{}, fmt.Errorf("recipe %q is missing response ID for regeneration", dismissed.Title)
			}
			slog.InfoContext(ctx, "dismissed recipe", "hash", dismissed.ComputeHash(), "title", dismissed.Title)

			instructions := append(slices.Clone(baseInstructions), "Passed on "+dismissed.Title)
			//todo add in alternate menu plan?
			recipe, err := g.aiClient.Regenerate(ctx, instructions, dismissed.ResponseID)
			if err != nil {
				return recipePipelineResult{}, err
			}
			recipe.OriginHash = hash
			recipe.ParentHash = dismissed.ComputeHash()
			return g.critiqueAndMaybeRetryRecipe(ctx, hash, *recipe)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}

		recipes := make([]ai.Recipe, 0, len(results)+len(p.Saved))
		discarded := make([]ai.Recipe, 0, len(results))
		for _, result := range results {
			recipes = append(recipes, result.Recipe)
			discarded = append(discarded, result.Discarded...)
		}

		recipes = append(recipes, p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return &ai.ShoppingList{
			Recipes:   recipes,
			Discarded: append(p.Dismissed, discarded...),
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

	menuPlan, err := g.aiClient.CreateMenuPlan(ctx, ingredients, p.Date, p.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to plan recipe variety: %w", err)
	}
	planCount := min(3, len(menuPlan.Plans))
	recipePlans, leftovers := menuPlan.Plans[:planCount], menuPlan.Plans[planCount:]

	results, err := parallelism.MapWithErrors(recipePlans, func(plan ai.RecipePlan) (recipePipelineResult, error) {
		recipe, err := g.aiClient.GenerateRecipe(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes, plan)
		if err != nil {
			return recipePipelineResult{}, err
		}
		// would prefer to do this deeper down in client like response id but have to pass in the hash
		recipe.OriginHash = hash
		return g.critiqueAndMaybeRetryRecipe(ctx, hash, *recipe)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	recipes := make([]ai.Recipe, 0, len(results))
	discarded := make([]ai.Recipe, 0)
	for _, result := range results {
		recipes = append(recipes, result.Recipe)
		discarded = append(discarded, result.Discarded...)
	}

	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return &ai.ShoppingList{
		Recipes:   recipes,
		Discarded: discarded,
		Plan:      &ai.MenuPlan{Plans: leftovers, Notes: menuPlan.Notes},
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

func (g *generatorService) critiqueAndMaybeRetryRecipe(ctx context.Context, hash string, recipe ai.Recipe) (recipePipelineResult, error) {
	if g.critiquer == nil {
		return recipePipelineResult{Recipe: recipe}, nil
	}
	ctx, span := tracer.Start(ctx, "recipes.critique.recipe")
	defer span.End()

	g.writeStatus(ctx, hash, status.Titles("Getting feedback on this recipe:", []ai.Recipe{recipe}))

	result := <-g.critiquer.CritiqueRecipe(ctx, recipe)
	if result.Recipe == nil {
		result.Recipe = &recipe
	}
	if result.Err != nil {
		slog.ErrorContext(ctx, "failed to critique recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "error", result.Err)
		return recipePipelineResult{Recipe: recipe}, nil
	}
	if result.Critique == nil || result.Critique.OverallScore >= critique.MinimumRecipeScore {
		return recipePipelineResult{Recipe: recipe}, nil
	}

	span.SetAttributes(attribute.Bool("regenaftercrique", true))
	slog.InfoContext(ctx, "low scoring recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)
	g.writeStatus(ctx, hash, status.Titles("Making adjustments to this recipe: ", []ai.Recipe{recipe}))

	if strings.TrimSpace(recipe.ResponseID) == "" {
		return recipePipelineResult{}, fmt.Errorf("recipe %q is missing response ID for critique retry", recipe.Title)
	}
	retry, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions(result), recipe.ResponseID)
	if err != nil {
		return recipePipelineResult{}, fmt.Errorf("failed to regenerate recipe %q from critique feedback: %w", recipe.Title, err)
	}
	retry.OriginHash = hash
	if parentHash := recipe.ComputeHash(); parentHash != "" && parentHash != retry.ComputeHash() {
		retry.ParentHash = parentHash
	}
	g.critiqueInBackground(ctx, *retry)

	return recipePipelineResult{
		Recipe:    *retry,
		Discarded: []ai.Recipe{recipe},
	}, nil
}

func (g *generatorService) critiqueInBackground(ctx context.Context, recipe ai.Recipe) {
	if g.critiquer == nil {
		return
	}
	results := g.critiquer.CritiqueRecipe(ctx, recipe)
	go func() {
		for result := range results {
			if result.Err != nil {
				slog.ErrorContext(ctx, "failed to critique retried recipe", "hash", recipe.ComputeHash(), "title", recipe.Title, "error", result.Err)
			}
		}
	}()
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
