package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type aiClient interface {
	GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error)
	Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	Ready(ctx context.Context) error
}

type ingredientSource interface {
	GetStaples(ctx context.Context, p *generatorParams) ([]ai.InputIngredient, error)
	GetIngredients(ctx context.Context, location string, f filter, skip int) ([]ai.InputIngredient, error)
}

type Generator struct {
	config             *config.Config
	aiClient           aiClient
	ingredientProvider ingredientSource
}

func NewGenerator(cfg *config.Config, cache cache.Cache) (generator, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	ingredientProvider, err := NewKrogerIngredientProvider(cfg, cache)
	if err != nil {
		return nil, err
	}

	return &Generator{
		config:             cfg,
		aiClient:           ai.NewClient(cfg.AI.APIKey, "TODOMODEL"),
		ingredientProvider: ingredientProvider,
	}, nil
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

type filter struct {
	Term   string   `json:"term,omitempty"`
	Brands []string `json:"brands,omitempty"`
	Frozen bool     `json:"frozen,omitempty"`
}

func Filter(term string, brands []string, frozen bool) filter {
	return filter{
		Term:   term,
		Brands: brands,
		Frozen: frozen,
	}
}

func (g *Generator) GetStaples(ctx context.Context, p *generatorParams) ([]ai.InputIngredient, error) {
	return g.ingredientProvider.GetStaples(ctx, p)
}

func (g *Generator) GetIngredients(ctx context.Context, location string, f filter, skip int) ([]ai.InputIngredient, error) {
	return g.ingredientProvider.GetIngredients(ctx, location, f, skip)
}

func (g *Generator) Ready(ctx context.Context) error {
	return g.aiClient.Ready(ctx)
}
