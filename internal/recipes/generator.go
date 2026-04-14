package recipes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
	PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error)
	Ready(ctx context.Context) error
}

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
	Ready(ctx context.Context) error
}

type ingredientio interface {
	SaveIngredients(ctx context.Context, hash string, ingredients []kroger.Ingredient) error
	IngredientsFromCache(ctx context.Context, hash string) ([]kroger.Ingredient, error)
}

type critiqueIO interface {
	SaveCritique(ctx context.Context, hash string, critique *ai.RecipeCritique) error
}

const minimumRecipeCritiqueScore = 8

type Generator struct {
	config          *config.Config
	aiClient        aiClient
	critiquer       recipeCritiquer
	staplesProvider staplesProvider
	io              ingredientio
	cio             critiqueIO // pull this out?
}

type allIO interface {
	ingredientio
	critiqueIO
}

func NewGenerator(cfg *config.Config, io allIO) (generatorPlus, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	stapesProvider, err := NewStaplesProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create staples provider: %w", err)
	}

	var critiquer recipeCritiquer
	if cfg.Gemini.IsEnabled() {
		critiquer = ai.NewCritiquer(cfg.Gemini.APIKey, cfg.Gemini.CritiqueModel)
	}

	return &Generator{
		io:              io,
		cio:             io, // pull this out?
		config:          cfg,
		aiClient:        ai.NewClient(cfg.AI.APIKey, "TODOMODEL"),
		critiquer:       critiquer,
		staplesProvider: stapesProvider,
	}, nil
}

func (g *Generator) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	var styles []string
	for _, style := range recipe.WineStyles {
		style = strings.TrimSpace(style)
		if style != "" { // would this ever happen?
			styles = append(styles, style)
		}
	}

	// whole foods search not actually implmented hard code categories
	if wholefoods.NewIdentityProvider().IsID(location) {
		styles = []string{"red-wine", "white-wine", "sparkling"} // rose
	}

	if len(styles) == 0 {
		return &ai.WineSelection{Commentary: "no wines styles for recipe", Wines: []ai.Ingredient{}}, nil
	}
	dateStr := date.Format("2006-01-02")
	logger := slog.With("location", location, "date", dateStr)

	wines, err := parallelism.Flatten(styles, func(style string) ([]kroger.Ingredient, error) {
		cacheKey := wineIngredientsCacheKey(style, location, date)
		winesOfStyle, err := g.io.IngredientsFromCache(ctx, cacheKey)
		if err == nil {
			logger.InfoContext(ctx, "Serving cached wines for style", "style", style, "count", len(winesOfStyle))
			return winesOfStyle, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			logger.ErrorContext(ctx, "Failed to read cached wines for style", "style", style, "error", err)
		}

		winesOfStyle, err = g.staplesProvider.GetIngredients(ctx, location, style, 0)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to get ingredients for wine style", "style", style, "error", err)
			return nil, fmt.Errorf("failed to get ingredients for style %q: %w", style, err)
		}
		logger.InfoContext(ctx, "Found wines.", "style", style, "count", len(winesOfStyle))

		if err := g.io.SaveIngredients(ctx, cacheKey, winesOfStyle); err != nil {
			logger.ErrorContext(ctx, "Failed to cache wines for style", "style", style, "error", err)
		}
		return winesOfStyle, nil
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
		// these should both always be true. Warn if not because its a caching bug?
		instructions := []string{p.Instructions}
		// TODO give more guidnance on how many recipes to generate here
		for _, dismissed := range p.Dismissed {
			instructions = append(instructions, "Passed on "+dismissed.Title)
		}
		for _, saved := range newlySaved(p.Saved, p.PriorSavedHashes) {
			instructions = append(instructions, "Enjoyed and saved so don't repeat: "+saved)
		}

		shoppingList, err := g.aiClient.Regenerate(ctx, instructions, p.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		shoppingList, err = g.critiqueAndMaybeRetry(ctx, shoppingList)
		if err != nil {
			return nil, err
		}
		// Include saved recipes in the shopping list
		shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

		slog.InfoContext(ctx, "regenerated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
		return shoppingList, nil
	}
	slog.InfoContext(ctx, "Generating recipes for location", "location", p.String())
	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}

	instructions := []string{p.Directive, p.Instructions}

	shoppingList, err := g.aiClient.GenerateRecipes(ctx, p.Location, ingredients, instructions, p.Date, p.LastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}
	shoppingList, err = g.critiqueAndMaybeRetry(ctx, shoppingList)
	if err != nil {
		return nil, err
	}
	// how to pipe this back to ai client? should ai client hjave its own critiquer or do we just call regenerate once?

	// should never happen? How do you get save on first generte?
	// shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

	// TODO this does not get saved in params and thus must be loaded from html
	// could update params after first generation or pregenerate before we save params.
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

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]kroger.Ingredient, error) {
	lochash := p.LocationHash()

	if cachedIngredients, err := g.io.IngredientsFromCache(ctx, lochash); err == nil {
		slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	ingredients, err := g.staplesProvider.FetchStaples(ctx, p.Location.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples for %s: %w", p.Location.ID, err)
	}
	// should this be pushed down into staple proivder? go off product id?
	ingredients = uniqueByDescription(ingredients)

	mutable.Shuffle(ingredients)

	if err := g.io.SaveIngredients(ctx, p.LocationHash(), ingredients); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return ingredients, nil
}

// TODO should we be going off product id instead?
func uniqueByDescription(ingredients []kroger.Ingredient) []kroger.Ingredient {
	return lo.UniqBy(ingredients, func(i kroger.Ingredient) string {
		return toStr(i.Description)
	})
}

func (g *Generator) Ready(ctx context.Context) error {
	if err := g.aiClient.Ready(ctx); err != nil {
		return err
	}
	if g.critiquer != nil {
		if err := g.critiquer.Ready(ctx); err != nil {
			return fmt.Errorf("gemini critique client not ready: %w", err)
		}
	}
	return nil
}

// this is a little expnsive so unlike ready above needs to be protected by a once by.
func (g *Generator) Watchdog(ctx context.Context) error {
	storeIDs := []string{
		"wholefoods_10153", // bellevue
		"safeway_490",      // bellevue
		"70500874",         // qfc in bellevue
		"starmarket_3566",  // boston
		"acmemarkets_806",  // newark
	}
	_, err := parallelism.Flatten(storeIDs, func(storeID string) ([]kroger.Ingredient, error) {
		// defeats point of watch dog to read from cache but we could write to it as a courtesy.
		return g.staplesProvider.FetchStaples(ctx, storeID)
	})

	return err
}

// toStr returns the string value if non-nil, or "empty" otherwise.
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

type recipeCritiqueResult struct {
	Recipe   *ai.Recipe // just here so we can give the model the title
	Critique *ai.RecipeCritique
}

func (g *Generator) critiqueAndMaybeRetry(ctx context.Context, shoppingList *ai.ShoppingList) (*ai.ShoppingList, error) {
	results, err := g.cacheRecipeCritiques(ctx, shoppingList.Recipes)
	if err != nil {
		return nil, fmt.Errorf("failed to cache recipe critiques: %w", err)
	}
	var garbage []recipeCritiqueResult
	var good []ai.Recipe
	for _, result := range results {
		if result.Critique.OverallScore >= minimumRecipeCritiqueScore {
			slog.InfoContext(ctx, "acceptable", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)

			good = append(good, *result.Recipe)
		} else {
			// if there are no issues should we still retry? wasted of tokens
			slog.InfoContext(ctx, "low scoring recipe", "hash", result.Recipe.ComputeHash(), "title", result.Recipe.Title, "score", result.Critique.OverallScore)
			garbage = append(garbage, result)
		}
	}
	if len(garbage) == 0 {
		return shoppingList, nil
	}
	slog.InfoContext(ctx, "regenerating recipes based on critique feedback", "garbage_count", len(garbage), "good_count", len(good))

	// store the garbage ones for reference

	if strings.TrimSpace(shoppingList.ConversationID) == "" {
		return nil, fmt.Errorf("conversation ID is required for critique retry")
	}

	retried, err := g.aiClient.Regenerate(ctx, critiqueRetryInstructions(garbage), shoppingList.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes from critique feedback: %w", err)
	}
	retried.Recipes = append(retried.Recipes, good...)
	shoppingList.Discarded = lo.Map(garbage, func(result recipeCritiqueResult, _ int) ai.Recipe {
		return *result.Recipe
	})

	// async as this is just debug not retrying twice yet.
	if _, err := g.cacheRecipeCritiques(ctx, retried.Recipes); err != nil {
		return nil, fmt.Errorf("failed to cache recipe critiques: %w", err)
	}
	return retried, nil
}

func (g *Generator) cacheRecipeCritiques(ctx context.Context, recipes []ai.Recipe) ([]recipeCritiqueResult, error) {
	if g.critiquer == nil || g.cio == nil {
		// yuck refactor tests to make this alway present
		return nil, nil
	}
	return parallelism.MapWithErrors(recipes, func(recipe ai.Recipe) (recipeCritiqueResult, error) {
		hash := recipe.ComputeHash()
		critique, err := g.critiquer.CritiqueRecipe(ctx, recipe)
		if err != nil {
			slog.ErrorContext(ctx, "failed to critique recipe", "recipe", recipe.Title, "hash", hash, "error", err)
			return recipeCritiqueResult{}, fmt.Errorf("critique recipe %q (%s): %w", recipe.Title, hash, err)
		}
		// should we background the saving of this? too fast to matter?
		if err := g.cio.SaveCritique(ctx, hash, critique); err != nil {
			slog.ErrorContext(ctx, "failed to cache recipe critique", "recipe", recipe.Title, "hash", hash, "error", err)
			return recipeCritiqueResult{}, fmt.Errorf("cache critique for recipe %q (%s): %w", recipe.Title, hash, err)
		}
		return recipeCritiqueResult{
			Recipe:   &recipe,
			Critique: critique,
		}, nil
	})
}

func critiqueRetryInstructions(results []recipeCritiqueResult) []string {
	revise := fmt.Sprintf("Revise and return exactly %d recipes as replacements for the low-scoring recipes listed below.", len(results))
	instructions := []string{revise}
	for _, result := range results {
		// do we care about summar or is it just a wast of tokens
		instructions = append(instructions, fmt.Sprintf(
			"Recipe %q scored %d/10.\n Issues: %s\n Suggested fixes: %s",
			result.Recipe.Title,
			result.Critique.OverallScore,
			// strings.TrimSpace(result.Critique.Summary),
			formatCritiqueIssues(result.Critique.Issues),
			formatSuggestedFixes(result.Critique.SuggestedFixes),
		))
	}
	return instructions
}

func formatCritiqueIssues(issues []ai.RecipeCritiqueIssue) string {
	if len(issues) == 0 {
		return "none listed."
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s", issue.Category, issue.Severity, strings.TrimSpace(issue.Detail)))
	}
	return strings.Join(parts, "; ")
}

func formatSuggestedFixes(fixes []string) string {
	if len(fixes) == 0 {
		return "none listed."
	}
	trimmed := make([]string, 0, len(fixes))
	for _, fix := range fixes {
		if fix = strings.TrimSpace(fix); fix != "" {
			trimmed = append(trimmed, fix)
		}
	}
	if len(trimmed) == 0 {
		return "none listed."
	}
	return strings.Join(trimmed, "; ")
}
