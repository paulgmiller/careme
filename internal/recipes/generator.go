package recipes

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/recipes/critique"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
	PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error)
}

type staplesService interface {
	GetStaples(ctx context.Context, p *GeneratorParams) ([]kroger.Ingredient, error)
	// only used for wine. Probably need a refactoro
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int, date time.Time) ([]kroger.Ingredient, error)
}

// TODO unexport?
type Generator struct {
	aiClient  aiClient
	critiquer critique.Service
	staples   staplesService
	statuses  generationStatusStore
}

func NewGenerator(aiClient aiClient, critiquer critique.Service, staples staplesService, statuses generationStatusStore) (*Generator, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is required")
	}
	if critiquer == nil {
		return nil, fmt.Errorf("critiquer is required")
	}
	if staples == nil {
		return nil, fmt.Errorf("staples service is required")
	}
	return &Generator{
		aiClient:  aiClient,
		critiquer: critiquer,
		staples:   staples,
		statuses:  statuses,
	}, nil
}

func (g *Generator) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
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

	wines, err := parallelism.Flatten(styles, func(style string) ([]kroger.Ingredient, error) {
		return g.staples.GetIngredients(ctx, location, style, 0, date)
	})
	if err != nil {
		return nil, err
	}

	if len(wines) == 0 {
		return &ai.WineSelection{Commentary: "no wines found", Wines: []ai.Ingredient{}}, nil
	}
	wines = uniqueByDescription(wines)

	selection, err := g.aiClient.PickWine(ctx, recipe, wines)
	if err != nil {
		return nil, err
	}
	return selection, nil
}

func (g *Generator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	hash := p.Hash()
	start := time.Now()

	if p.ConversationID != "" && (p.Instructions != "" || len(p.Saved) > 0 || len(p.Dismissed) > 0) {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "conversation_id", p.ConversationID)
		// should never get a conversation id without instructions or saved/dismissed
		// could assert or warn on that
		instructions := []string{p.Instructions}
		for _, dismissed := range p.Dismissed {
			instructions = append(instructions, "Passed on "+dismissed.Title)
		}
		for _, saved := range newlySaved(p.Saved, p.PriorSavedHashes) {
			instructions = append(instructions, "Enjoyed and saved so don't repeat: "+saved)
		}
		// TODO more guidance on how many recipes to generate?

		shoppingList, err := g.aiClient.Regenerate(ctx, instructions, p.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		g.updateGenerationStatus(ctx, hash, generationStageRecipesGenerated)
		shoppingList, err = g.critiqueAndMaybeRetry(ctx, hash, shoppingList)
		if err != nil {
			return nil, err
		}
		shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return shoppingList, nil
	}

	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.staples.GetStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}
	g.updateGenerationStatus(ctx, hash, generationStageIngredientsReady)

	instructions := []string{p.Directive, p.Instructions}
	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	g.updateGenerationStatus(ctx, hash, generationStageRecipesGenerated)
	shoppingList, err = g.critiqueAndMaybeRetry(ctx, hash, shoppingList)
	if err != nil {
		return nil, err
	}

	p.ConversationID = shoppingList.ConversationID
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
}

func (g *Generator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	return g.aiClient.AskQuestion(ctx, question, conversationID)
}

func (g *Generator) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	return g.aiClient.GenerateRecipeImage(ctx, recipe)
}

func uniqueByDescription(ingredients []kroger.Ingredient) []kroger.Ingredient {
	return lo.UniqBy(ingredients, func(i kroger.Ingredient) string {
		return toStr(i.Description)
	})
}

func toStr(s *string) string {
	if s == nil {
		return "empty"
	}
	return *s
}

func wineIngredientsCacheKey(style, location string, date time.Time) string {
	normalizedStyle := strings.ToLower(strings.TrimSpace(style))
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, location))
	lo.Must(io.WriteString(fnv, date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, normalizedStyle))
	return "wines/" + base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
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

func (g *Generator) critiqueAndMaybeRetry(ctx context.Context, hash string, shoppingList *ai.ShoppingList) (*ai.ShoppingList, error) {
	if g.critiquer == nil {
		g.updateGenerationStatus(ctx, hash, generationStageComplete)
		return shoppingList, nil
	}

	results := g.critiquer.CritiqueRecipes(ctx, shoppingList.Recipes)
	good, garbage := critique.Split(ctx, results, critique.MinimumRecipeScore)
	for _, result := range garbage {
		slog.InfoContext(ctx, "low scoring recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)
	}
	if len(garbage) == 0 {
		g.updateGenerationStatus(ctx, hash, generationStageComplete)
		return shoppingList, nil
	}
	slog.InfoContext(ctx, "regenerating recipes based on critique feedback", "garbage_count", len(garbage), "good_count", len(good))
	g.updateGenerationStatus(ctx, hash, generationStageCritiqueRetrying)

	if strings.TrimSpace(shoppingList.ConversationID) == "" {
		return nil, fmt.Errorf("conversation ID is required for critique retry")
	}

	// we could also just give all feedback back if any are below score
	shoppingList, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions(garbage), shoppingList.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes from critique feedback: %w", err)
	}
	newRecipes := shoppingList.Recipes
	shoppingList.Recipes = append(shoppingList.Recipes, good...)
	shoppingList.Discarded = lo.Map(garbage, func(result critique.Result, _ int) ai.Recipe {
		return *result.Recipe
	})

	_ = g.critiquer.CritiqueRecipes(ctx, newRecipes)
	g.updateGenerationStatus(ctx, hash, generationStageComplete)
	return shoppingList, nil
}

func (g *Generator) updateGenerationStatus(ctx context.Context, hash string, stage string) {
	if g == nil || g.statuses == nil || strings.TrimSpace(hash) == "" {
		return
	}
	if err := g.statuses.SaveGenerationStatus(ctx, hash, newGenerationStatus(stage)); err != nil {
		slog.ErrorContext(ctx, "failed to save generation status", "hash", hash, "stage", stage, "error", err)
	}
}
