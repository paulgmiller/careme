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
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
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
		instructions := regenerateInstructions(p)

		g.writeStatus(ctx, hash, status.Regen(p.Instructions, p.Dismissed))

		replacements, err := parallelism.MapWithErrors(p.Dismissed, func(dismissed ai.Recipe) (ai.Recipe, error) {
			if strings.TrimSpace(dismissed.ResponseID) == "" {
				return ai.Recipe{}, fmt.Errorf("recipe %q is missing response ID for regeneration", dismissed.Title)
			}
			slog.InfoContext(ctx, "dismissed recipe", "hash", dismissed.ComputeHash(), "title", dismissed.Title)
			recipe, err := g.aiClient.Regenerate(ctx, instructions, dismissed.ResponseID)
			if err != nil {
				return ai.Recipe{}, err
			}
			recipe.OriginHash = hash
			if parentHash := dismissed.ComputeHash(); parentHash != "" && parentHash != recipe.ComputeHash() {
				recipe.ParentHash = parentHash
			}
			return *recipe, nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}

		recipes, discarded, err := g.critiqueAndMaybeRetry(ctx, hash, replacements)
		if err != nil {
			return nil, err
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

	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	// would prefer to do this deepe down in client like response id but have to pass in the hash
	for i := range shoppingList.Recipes {
		shoppingList.Recipes[i].OriginHash = hash
	}

	recipes, discarded, err := g.critiqueAndMaybeRetry(ctx, hash, shoppingList.Recipes)
	if err != nil {
		return nil, err
	}
	shoppingList.Recipes = recipes
	shoppingList.Discarded = discarded

	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
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
	for _, dismissed := range p.Dismissed {
		instructions = append(instructions, "Passed on "+dismissed.Title)
	}
	for _, saved := range newlySaved(p.Saved, p.PriorSavedHashes) {
		instructions = append(instructions, "Enjoyed and saved so don't repeat: "+saved)
	}
	return instructions
}

func (g *generatorService) critiqueAndMaybeRetry(ctx context.Context, hash string, recipes []ai.Recipe) ([]ai.Recipe, []ai.Recipe, error) {
	if g.critiquer == nil {
		return recipes, nil, nil
	}
	ctx, span := tracer.Start(ctx, "recipes.critique")
	defer span.End()

	g.writeStatus(ctx, hash, status.Titles("Getting feeeback on these recipes:", recipes))

	accepted := make([]ai.Recipe, 0, len(recipes))
	discarded := make([]ai.Recipe, 0)
	for _, recipe := range recipes {
		result := <-g.critiquer.CritiqueRecipe(ctx, recipe)
		if result.Recipe == nil {
			result.Recipe = &recipe
		}
		if result.Err != nil {
			slog.ErrorContext(ctx, "failed to critique recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "error", result.Err)
			accepted = append(accepted, recipe)
			continue
		}
		if result.Critique == nil || result.Critique.OverallScore >= critique.MinimumRecipeScore {
			accepted = append(accepted, recipe)
			continue
		}

		span.SetAttributes(attribute.Bool("regenaftercrique", true))
		slog.InfoContext(ctx, "low scoring recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)
		g.writeStatus(ctx, hash, status.Titles("Making adjustments to these recipes: ", []ai.Recipe{recipe}))

		if strings.TrimSpace(recipe.ResponseID) == "" {
			return nil, nil, fmt.Errorf("recipe %q is missing response ID for critique retry", recipe.Title)
		}
		retry, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions([]critique.Result{result}), recipe.ResponseID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to regenerate recipe %q from critique feedback: %w", recipe.Title, err)
		}
		retry.OriginHash = hash
		if parentHash := recipe.ComputeHash(); parentHash != "" && parentHash != retry.ComputeHash() {
			retry.ParentHash = parentHash
		}
		accepted = append(accepted, *retry)
		discarded = append(discarded, recipe)

		retryResult := <-g.critiquer.CritiqueRecipe(ctx, *retry)
		if retryResult.Err != nil {
			slog.ErrorContext(ctx, "failed to critique retried recipe", "hash", retry.ComputeHash(), "title", retry.Title, "error", retryResult.Err)
		}
	}

	return accepted, discarded, nil
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
