package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/history"
	"careme/internal/kroger"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
)

type Generator struct {
	config         *config.Config
	aiClient       *ai.Client
	krogerClient   kroger.ClientWithResponsesInterface //probably need only subset
	historyStorage *history.HistoryStorage
}

type GeneratedRecipes struct {
	Recipes []history.Recipe `json:"recipes"`
}

func NewGenerator(cfg *config.Config) (*Generator, error) {

	basicAuth, err := securityprovider.NewSecurityProviderBasicAuth(cfg.Kroger.ClientID, cfg.Kroger.ClientSecret)
	if err != nil {
		return nil, err
	}

	client, err := kroger.NewClientWithResponses("https://api.kroger.com/", kroger.WithRequestEditorFn(basicAuth.Intercept))
	if err != nil {
		return nil, err
	}
	return &Generator{
		config:         cfg,
		aiClient:       ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model),
		krogerClient:   client,
		historyStorage: history.NewHistoryStorage(cfg.History.StoragePath, cfg.History.RetentionDays),
	}, nil
}

func (g *Generator) GenerateWeeklyRecipes(location string) ([]history.Recipe, error) {
	log.Printf("Generating recipes for location: %s", location)

	saleIngredients, err := g.getSaleIngredients(location, "steak")
	if err != nil {
		log.Printf("Warning: Could not fetch sale ingredients: %v", err)
		saleIngredients = []string{}
	}

	previousRecipes, err := g.getPreviousRecipes()
	if err != nil {
		log.Printf("Warning: Could not fetch recipe history: %v", err)
		previousRecipes = []string{}
	}

	log.Printf("Found %d sale ingredients, %d previous recipes",
		len(saleIngredients), len(previousRecipes))

	response, err := g.aiClient.GenerateRecipes(location, saleIngredients, previousRecipes)
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

func (g *Generator) getSaleIngredients(location, term string) ([]string, error) {
	products, err := g.krogerClient.ProductSearchWithResponse(context.TODO(), &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &term,
	})
	if err != nil {
		return nil, err
	}

	if products.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("Got %d code from kroger", products.StatusCode())
	}

	var ingredients []string
	for _, product := range *products.JSON200.Data {
		for _, item := range *product.Items {
			//does just giving the model json work better here?
			ingredients = append(ingredients, fmt.Sprintf("%s %s price %s sale %s", product.Description, *item.Size, item.Price.Regular, item.Price.Promo))
		}
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
