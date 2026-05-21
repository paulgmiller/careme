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
	CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error)
	RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error)
	GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.Recipe, error)
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

var menuPlanResponseIDBackCompatLastDate = time.Date(2026, time.May, 22, 0, 0, 0, 0, time.UTC)

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
	enrichIngredientsMetadata(selection.Wines, inputIngredientMap(wines))
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

		if len(p.Dismissed) == 0 {
			// should disallow this in ui?
			slog.ErrorContext(ctx, "regenerated chat with only saved recipes", "location", p.String(), "duration", time.Since(start), "hash", hash)
			return &ai.ShoppingList{
				Recipes: p.Saved,
			}, nil
		}

		regenInstructions := regenerateInstructions(p)

		menuPlan, err := g.replacementMenuPlan(ctx, p, regenInstructions, len(p.Dismissed))
		if err != nil {
			return nil, fmt.Errorf("failed to plan recipe replacements: %w", err)
		}
		if len(menuPlan.Plans) != len(p.Dismissed) {
			return nil, fmt.Errorf("failed to plan recipe replacements: %w", err)
		}
		// does it matter how we associate these?
		type plannedRegeneration struct {
			plan       ai.RecipePlan
			responseID string
		}
		var replacements []plannedRegeneration
		for i, plan := range menuPlan.Plans {
			if strings.TrimSpace(p.Dismissed[i].ResponseID) == "" {
				return nil, fmt.Errorf("recipe %q is missing response ID for regeneration", p.Dismissed[i].Title)
			}
			replacements = append(replacements, plannedRegeneration{
				plan:       plan,
				responseID: p.Dismissed[i].ResponseID,
			})
		}

		g.writeStatus(ctx, hash, menuPlan.String())

		// this SHOULD hit the cache and we could do it in parallel with menu planning
		ingredients, err := g.staples.FetchStaples(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("failed to get staples: %w", err)
		}
		ingredients = lo.Filter(ingredients, func(ing ai.InputIngredient, _ int) bool {
			// TODO make configurable?
			return ing.Grade == nil || ing.Grade.Score > 6
		})
		ingMap := inputIngredientMap(ingredients)

		results, err := parallelism.MapWithErrors(replacements, func(replacement plannedRegeneration) (*ai.Recipe, error) {
			ctx, span := tracer.Start(ctx, "recipes.regenerate.single")
			defer span.End()

			instructions := append(slices.Clone(regenInstructions), replacement.plan.Instructions()...)
			recipe, err := g.aiClient.Regenerate(ctx, instructions, replacement.responseID)
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
			Plan:    menuPlan,
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
	ingMap := inputIngredientMap(ingredients)

	g.writeStatus(ctx, hash, status.Ingredients(ingredients, ogCount))
	mutable.Shuffle(ingredients)

	instructions := []string{p.Directive, p.Instructions}

	// 3 is arbitrary let user decide?
	menuPlan, err := g.aiClient.CreateMenuPlan(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to plan recipe variety: %w", err)
	}
	g.writeStatus(ctx, hash, menuPlan.String())

	results, err := parallelism.MapWithErrors(menuPlan.Plans, func(plan ai.RecipePlan) (*ai.Recipe, error) {
		ctx, span := tracer.Start(ctx, "recipes.generate.single")
		defer span.End()
		recipeInstructions := append(slices.Clone(instructions), plan.Instructions()...)
		recipe, err := g.aiClient.GenerateRecipe(ctx, p.Location, ingredients, recipeInstructions, p.Date, p.LastRecipes)
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

func (g *generatorService) replacementMenuPlan(ctx context.Context, p *generatorParams, instructions []string, count int) (*ai.MenuPlan, error) {
	if strings.TrimSpace(p.PreviousMenuPlanResponseID) != "" {
		return g.aiClient.RegenerateMenuPlan(ctx, instructions, p.PreviousMenuPlanResponseID, count)
	}
	if afterMenuPlanResponseIDBackCompatLastDate(p.Date) {
		return nil, fmt.Errorf("missing previous menu plan response ID for menu date %s", p.Date.Format("2006-01-02"))
	}
	// Backward compatibility for cached shopping lists created before menu plan response IDs were persisted.
	slog.WarnContext(ctx, "no menuplan on regen")
	return backCompatMenuPlan(count), nil
}

func afterMenuPlanResponseIDBackCompatLastDate(date time.Time) bool {
	year, month, day := date.Date()
	lastYear, lastMonth, lastDay := menuPlanResponseIDBackCompatLastDate.Date()
	if year != lastYear {
		return year > lastYear
	}
	if month != lastMonth {
		return month > lastMonth
	}
	return day > lastDay
}

func backCompatMenuPlan(count int) *ai.MenuPlan {
	plans := make([]ai.RecipePlan, 0, count)
	for range count {
		plans = append(plans, ai.RecipePlan{
			Cuisine:          "anything",
			AnchorIngredient: "anything",
			Technique:        "anything",
		})
	}
	return &ai.MenuPlan{Plans: plans}
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
	if g.critiquer == nil {
		return recipe, nil
	}
	ctx, span := tracer.Start(ctx, "recipes.critique.recipe")
	defer span.End()

	g.writeStatus(ctx, hash, "Getting feedback on "+recipe.Title+"\n")

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
	g.writeStatus(ctx, hash, "Adjusting "+recipe.Title+"\n")

	// panic?
	if strings.TrimSpace(recipe.ResponseID) == "" {
		return nil, fmt.Errorf("recipe %q is missing response ID for critique retry", recipe.Title)
	}
	retry, err := g.aiClient.Regenerate(ctx, critique.RetryInstructions(result), recipe.ResponseID)
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
