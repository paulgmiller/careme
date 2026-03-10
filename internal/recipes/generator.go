package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/wholefoods"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	PickWine(ctx context.Context, conversationID string, recipeTitle string, wines []kroger.Ingredient) (*ai.WineSelection, error)
	Ready(ctx context.Context) error
}

type ingredientio interface {
	SaveIngredients(ctx context.Context, hash string, ingredients []kroger.Ingredient) error
	IngredientsFromCache(ctx context.Context, hash string) ([]kroger.Ingredient, error)
}

type Generator struct {
	config          *config.Config
	aiClient        aiClient
	staplesProvider staplesProvider
	io              ingredientio
}

func NewGenerator(cfg *config.Config, io ingredientio) (generator, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	stapesProvider, err := NewStaplesProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create staples provider: %w", err)
	}

	return &Generator{
		io:              io,
		config:          cfg,
		aiClient:        ai.NewClient(cfg.AI.APIKey, "TODOMODEL"),
		staplesProvider: stapesProvider,
	}, nil
}

func (g *Generator) PickAWine(ctx context.Context, conversationID string, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	var styles []string
	for _, style := range recipe.WineStyles {
		style = strings.TrimSpace(style)
		if style != "" { //would this ever happen?
			styles = append(styles, style)
		}
	}

	//whole foods search not actually implmented hard code categories
	if wholefoods.NewIdentityProvider().IsID(location) {
		styles = []string{"red-wine", "white-wine", "sparkling"} //rose
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

	selection, err := g.aiClient.PickWine(ctx, conversationID, recipe.Title, wines)
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
		for _, dismissed := range p.Dismissed {
			instructions = append(instructions, "Passed on "+dismissed.Title)
		}

		shoppingList, err := g.aiClient.Regenerate(ctx, instructions, p.ConversationID)
		if err != nil {
			return nil, fmt.Errorf("failed to regenerate recipes with AI: %w", err)
		}
		// want to add saved to insructions but only once. TODO
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

	// should never happen? How do you get save on first generte?
	// shoppingList.Recipes = append(shoppingList.Recipes, p.Saved...)

	//TODO this does not get saved in params and thus must be loaded from html
	// could update params after first generation or pregenerate before we save params.
	p.ConversationID = shoppingList.ConversationID
	slog.InfoContext(ctx, "generated chat", "location", p.String(), "duration", time.Since(start), "hash", hash)
	return shoppingList, nil
}

func (g *Generator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	return g.aiClient.AskQuestion(ctx, question, conversationID)
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
		return nil, fmt.Errorf("failed to get ingredients for staples: %w", err)
	}
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
	return g.aiClient.Ready(ctx)
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
