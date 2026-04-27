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
	"careme/internal/telemetry"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
	"go.opentelemetry.io/otel/attribute"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error)
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
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

func (g *generatorService) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (selection *ai.WineSelection, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.wine.pick")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.String("location.provider", safeStaplesSignatureForLocation(location)))

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
		span.SetAttributes(attribute.Int("wine.style_count", 0), attribute.Int("wine.candidate_count", 0))
		return &ai.WineSelection{Commentary: "no wines styles for recipe", Wines: []ai.Ingredient{}}, nil
	}
	span.SetAttributes(attribute.Int("wine.style_count", len(styles)))

	wines, err := parallelism.Flatten(styles, func(style string) ([]ai.InputIngredient, error) {
		return g.staples.GetIngredients(ctx, location, style, 0, date)
	})
	if err != nil {
		return nil, err
	}

	if len(wines) == 0 {
		span.SetAttributes(attribute.Int("wine.candidate_count", 0))
		return &ai.WineSelection{Commentary: "no wines found", Wines: []ai.Ingredient{}}, nil
	}
	wines = lo.UniqBy(wines, func(i ai.InputIngredient) string {
		return i.ProductID
	})
	span.SetAttributes(attribute.Int("wine.candidate_count", len(wines)))

	aiCtx, aiSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ai.pick_wine")
	selection, err = g.aiClient.PickWine(aiCtx, recipe, wines)
	telemetry.EndResult(aiSpan, err)
	if err != nil {
		return nil, err
	}
	return selection, nil
}

func (g *generatorService) GenerateRecipes(ctx context.Context, p *generatorParams) (shoppingList *ai.ShoppingList, err error) {
	hash := p.Hash()
	start := time.Now()
	mode := "initial"
	if p.ResponseID != "" && (p.Instructions != "" || len(p.Saved) > 0 || len(p.Dismissed) > 0) {
		mode = "regenerate"
	}
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.generate")
	defer telemetry.End(span, &err)
	span.SetAttributes(
		attribute.String("generation.mode", mode),
		attribute.Bool("generation.response_id.present", p.ResponseID != ""),
		attribute.Int("generation.saved_count", len(p.Saved)),
		attribute.Int("generation.dismissed_count", len(p.Dismissed)),
		attribute.Int("generation.prior_saved_count", len(p.PriorSavedHashes)),
		attribute.Int("generation.last_recipe_count", len(p.LastRecipes)),
		attribute.String("location.provider", safeStaplesSignatureForLocation(p.Location.ID)),
	)

	// if we have a response id one of the three should be true? Or did they just not care and hit try again?
	if p.ResponseID != "" && (p.Instructions != "" || len(p.Saved) > 0 || len(p.Dismissed) > 0) {
		slog.InfoContext(ctx, "Regenerating recipes for location", "location", p.String(), "response_id", p.ResponseID)
		instructions := regenerateInstructions(p)

		span.SetAttributes(attribute.Int("generation.instruction_count", len(instructions)))
		aiCtx, aiSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ai.regenerate")
		shoppingList, err = g.aiClient.Regenerate(aiCtx, instructions, p.ResponseID)
		telemetry.EndResult(aiSpan, err)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		span.SetAttributes(attribute.Int("generation.recipe_count", len(shoppingList.Recipes)))
		// would prefer to do this deepe down in client
		for i := range shoppingList.Recipes {
			shoppingList.Recipes[i].OriginHash = hash
		}

		shoppingList, err = g.critiqueAndMaybeRetry(ctx, hash, shoppingList)
		if err != nil {
			return nil, err
		}
		span.SetAttributes(attribute.Int("generation.final_recipe_count", len(shoppingList.Recipes)))

		shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return shoppingList, nil
	}

	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.staples.FetchStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}
	g.writeStatus(ctx, hash, fmt.Sprintf("Looking through %d ingredients", len(ingredients)))
	span.SetAttributes(attribute.Int("generation.ingredient_count", len(ingredients)))
	ingredients = lo.Filter(ingredients, func(ing ai.InputIngredient, _ int) bool {
		// TODO make configurable?
		return ing.Grade == nil || ing.Grade.Score > 5
	})
	span.SetAttributes(attribute.Int("generation.filtered_ingredient_count", len(ingredients)))
	mutable.Shuffle(ingredients)

	instructions := []string{p.Directive, p.Instructions}
	span.SetAttributes(attribute.Int("generation.instruction_count", len(instructions)))
	aiCtx, aiSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ai.generate")
	shoppingList, err = g.aiClient.GenerateRecipes(aiCtx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
	telemetry.EndResult(aiSpan, err)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	span.SetAttributes(attribute.Int("generation.recipe_count", len(shoppingList.Recipes)))
	// would prefer to do this deepe down in client like response id but have to pass in the hash
	for i := range shoppingList.Recipes {
		shoppingList.Recipes[i].OriginHash = hash
	}

	shoppingList, err = g.critiqueAndMaybeRetry(ctx, hash, shoppingList)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Int("generation.final_recipe_count", len(shoppingList.Recipes)))

	p.ResponseID = shoppingList.ResponseID
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
}

// generator not prociding a lot of value here. Should sever just hold an ai client?
func (g *generatorService) AskQuestion(ctx context.Context, question string, previousResponseID string) (response *ai.QuestionResponse, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.question")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.Bool("question.response_id.present", strings.TrimSpace(previousResponseID) != ""))
	return g.aiClient.AskQuestion(ctx, question, previousResponseID)
}

func (g *generatorService) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (image *ai.GeneratedImage, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.image.generate")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.Int("recipe.instruction_count", len(recipe.Instructions)))
	return g.aiClient.GenerateRecipeImage(ctx, recipe)
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

func titles(prefix string, recipes []ai.Recipe) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("\n")
	for _, r := range recipes {
		b.WriteString(r.Title)
		b.WriteString("\n")
	}
	return b.String()
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

func (g *generatorService) critiqueAndMaybeRetry(ctx context.Context, hash string, shoppingList *ai.ShoppingList) (result *ai.ShoppingList, err error) {
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.critique_and_retry")
	defer telemetry.End(span, &err)
	if shoppingList != nil {
		span.SetAttributes(attribute.Int("critique.recipe_count", len(shoppingList.Recipes)))
	}
	if g.critiquer == nil {
		return shoppingList, nil
	}
	g.writeStatus(ctx, hash, titles("Getting feeeback on these recipes:", shoppingList.Recipes))
	results := g.critiquer.CritiqueRecipes(ctx, shoppingList.Recipes)
	good, garbage := critique.Split(ctx, results, critique.MinimumRecipeScore)
	for _, result := range garbage {
		slog.InfoContext(ctx, "low scoring recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)
	}
	if len(garbage) == 0 {
		span.SetAttributes(attribute.Int("critique.accepted_count", len(good)), attribute.Int("critique.retry_count", 0))
		return shoppingList, nil
	}
	span.SetAttributes(attribute.Int("critique.accepted_count", len(good)), attribute.Int("critique.retry_count", len(garbage)))
	slog.InfoContext(ctx, "Regenerating recipes based on critique feedback:", "garbage_count", len(garbage), "good_count", len(good))
	garbageRecipes := lo.Map(garbage, func(r critique.Result, _ int) ai.Recipe { return *r.Recipe })
	g.writeStatus(ctx, hash, titles("Making adjustments to these recipes: ", garbageRecipes))

	if strings.TrimSpace(shoppingList.ResponseID) == "" {
		return nil, fmt.Errorf("response ID is required for critique retry")
	}

	// we could also just give all feedback back if any are below score
	aiCtx, aiSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ai.critique_retry")
	shoppingList, err = g.aiClient.Regenerate(aiCtx, critique.RetryInstructions(garbage), shoppingList.ResponseID)
	telemetry.EndResult(aiSpan, err)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes from critique feedback: %w", err)
	}
	span.SetAttributes(attribute.Int("critique.retry_result_count", len(shoppingList.Recipes)))
	for i := range shoppingList.Recipes {
		shoppingList.Recipes[i].OriginHash = hash
	}
	newRecipes := shoppingList.Recipes
	linkToParents(garbage, recipePtrs(newRecipes))
	shoppingList.Recipes = append(shoppingList.Recipes, good...)
	shoppingList.Discarded = lo.Map(garbage, func(result critique.Result, _ int) ai.Recipe {
		return *result.Recipe
	})

	_ = g.critiquer.CritiqueRecipes(ctx, newRecipes)
	// no point in upating as we're async here g.updateGenerationStatus(ctx, hash, "")
	return shoppingList, nil
}

func safeStaplesSignatureForLocation(locationID string) (signature string) {
	defer func() {
		if recover() != nil {
			signature = "unknown"
		}
	}()
	return staplesSignatureForLocation(locationID)
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
