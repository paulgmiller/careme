package recipes

import (
	"encoding/json"
	"fmt"
	"log"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/history"
	"careme/internal/ingredients"
	"careme/internal/kroger"
)

type Generator struct {
	config          *config.Config
	aiClient        *ai.Client
	krogerClient    *kroger.Client
	seasonalClient  *ingredients.SeasonalClient
	historyStorage  *history.HistoryStorage
}

type GeneratedRecipes struct {
	Recipes []history.Recipe `json:"recipes"`
}

func NewGenerator(cfg *config.Config) *Generator {
	return &Generator{
		config:          cfg,
		aiClient:        ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model),
		krogerClient:    kroger.NewClient(cfg.Kroger.MCPServerURL, cfg.Kroger.APIKey),
		seasonalClient:  ingredients.NewSeasonalClient(cfg.Epicurious.APIEndpoint, cfg.Epicurious.APIKey),
		historyStorage:  history.NewHistoryStorage(cfg.History.StoragePath, cfg.History.RetentionDays),
	}
}

func (g *Generator) GenerateWeeklyRecipes(location string) ([]history.Recipe, error) {
	log.Printf("Generating recipes for location: %s", location)

	availableIngredients, err := g.getAvailableIngredients(location)
	if err != nil {
		log.Printf("Warning: Could not fetch available ingredients: %v", err)
		availableIngredients = []string{}
	}

	seasonalIngredients, err := g.getSeasonalIngredients(location)
	if err != nil {
		log.Printf("Warning: Could not fetch seasonal ingredients: %v", err)
		seasonalIngredients = []string{}
	}

	previousRecipes, err := g.getPreviousRecipes()
	if err != nil {
		log.Printf("Warning: Could not fetch recipe history: %v", err)
		previousRecipes = []string{}
	}

	log.Printf("Found %d available ingredients, %d seasonal ingredients, %d previous recipes",
		len(availableIngredients), len(seasonalIngredients), len(previousRecipes))

	response, err := g.aiClient.GenerateRecipes(location, availableIngredients, seasonalIngredients, previousRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	recipes, err := g.parseAIResponse(response, location)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	if err := g.historyStorage.SaveRecipes(recipes); err != nil {
		log.Printf("Warning: Could not save recipes to history: %v", err)
	}

	return recipes, nil
}

func (g *Generator) getAvailableIngredients(location string) ([]string, error) {
	commonIngredients := []string{
		"chicken", "beef", "pork", "salmon", "eggs",
		"onions", "garlic", "potatoes", "carrots", "celery",
		"tomatoes", "bell peppers", "broccoli", "spinach", "lettuce",
		"rice", "pasta", "bread", "milk", "cheese", "butter",
	}

	products, err := g.krogerClient.GetFreshIngredients(location, commonIngredients)
	if err != nil {
		return nil, err
	}

	var available []string
	for _, product := range products {
		if product.Available && product.Fresh {
			available = append(available, product.Name)
		}
	}

	return available, nil
}

func (g *Generator) getSeasonalIngredients(location string) ([]string, error) {
	seasonalItems, err := g.seasonalClient.GetSeasonalIngredients(location)
	if err != nil {
		return nil, err
	}

	var ingredients []string
	for _, item := range seasonalItems {
		ingredients = append(ingredients, item.Name)
	}

	return ingredients, nil
}

func (g *Generator) getPreviousRecipes() ([]string, error) {
	return g.historyStorage.GetRecipeNames(14) // Last 2 weeks
}

func (g *Generator) parseAIResponse(response, location string) ([]history.Recipe, error) {
	var generatedRecipes GeneratedRecipes
	if err := json.Unmarshal([]byte(response), &generatedRecipes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal AI response: %w", err)
	}

	if len(generatedRecipes.Recipes) != 4 {
		return nil, fmt.Errorf("expected 4 recipes, got %d", len(generatedRecipes.Recipes))
	}

	var recipes []history.Recipe
	for i, recipe := range generatedRecipes.Recipes {
		recipe.ID = fmt.Sprintf("recipe_%d", i+1)
		recipe.Location = location
		recipes = append(recipes, recipe)
	}

	return recipes, nil
}