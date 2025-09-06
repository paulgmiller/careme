package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"

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

	bearer, err := kroger.GetOAuth2Token(context.TODO(), cfg.Kroger.ClientID, cfg.Kroger.ClientSecret)
	if err != nil {
		return nil, err
	}

	bearerAuth, err := securityprovider.NewSecurityProviderBearerToken(bearer)
	if err != nil {
		return nil, err
	}

	// Add LoggingDoer to log all requests/responses
	//loggingDoer := &kroger.LoggingDoer{Wrapped: http.DefaultClient}
	client, err := kroger.NewClientWithResponses("https://api.kroger.com/v1",
		kroger.WithRequestEditorFn(bearerAuth.Intercept),
	//	kroger.WithHTTPClient(loggingDoer),
	)
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

	/*previousRecipes, err := g.getPreviousRecipes()
	if err != nil {
		log.Printf("Warning: Could not fetch recipe history: %v", err)
		previousRecipes = []string{}
	}*/

	ingredients, err := g.GetStaples(location)
	if err != nil {
		return nil, fmt.Errorf("failed to get staples: %w", err)
	}

	//log.Printf("Found %d sale ingredients, %d previous recipes", 		len(ingredients), len(previousRecipes))

	response, err := g.aiClient.GenerateRecipes(location, ingredients, previousRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes with AI: %w", err)
	}

	/*recipes, err := g.parseAIResponse(response, location)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	if err := g.historyStorage.SaveRecipes(recipes); err != nil {
		log.Printf("Warning: Could not save recipes to history: %v", err)
	}
	*/

	return recipes, nil
}

type filter struct {
	Term   string
	Brands []string
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (g *Generator) GetStaples(location string) ([]string, error) {
	categories := []filter{
		{
			Term:   "lamb",
			Brands: []string{"Simple Truth"},
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms"},
		},
		{
			Term: "beef",
		},
		{
			Term: "fish",
		},
		{
			Term: "pork",
		},
		{
			Term: "chicken",
		},
		{
			Term: "shellfish",
		},
		{
			Term: "produce vegetable",
		},
	}
	var ingredients []string
	for _, category := range categories {
		cingredients, err := g.GetIngredients(location, category, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to get ingredients: %w", err)
		}
		ingredients = append(ingredients, cingredients...)
	}
	return ingredients, nil
}

// move to krogrer client as everyone will be differnt here?
func (g *Generator) GetIngredients(location string, f filter, skip int) ([]string, error) {
	limit := 50
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	//brand := "empty" doesn't work have to check for nil
	//fulfillment := "ais" drmatically shortens?
	products, err := g.krogerClient.ProductSearchWithResponse(context.TODO(), &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &f.Term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
		//FilterBrand:      &brand,
		//FilterFulfillment: &fulfillment,
	})
	if err != nil {
		return nil, fmt.Errorf("failed on product searchwith response %w", err)
	}

	if products.StatusCode() != http.StatusOK {
		fmt.Printf("Kroger ProductSearchWithResponse returned status: %d\n", products.StatusCode())
		return nil, fmt.Errorf("got %d code from kroger", products.StatusCode())
	}

	var ingredients []string

	for _, product := range *products.JSON200.Data {
		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) {
			continue
		}
		//end up with a bunch of frozen chicken with out this.
		if slices.Contains(*product.Categories, "Frozen") {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				// todo what does this mean?
				continue
			}

			//does just giving the model json work better here?
			ingredient := fmt.Sprintf(
				"%s %s price %.2f",
				//toStr(product.Brand),
				toStr(product.Description),
				toStr(item.Size),
				toFloat32(item.Price.Regular),
			)

			if toFloat32(item.Price.Promo) > 0.0 {
				ingredient += fmt.Sprintf(" sale %.2f", toFloat32(item.Price.Promo))
			}
			ingredients = append(ingredients, ingredient)
			//strings.Join(*product.Categories, ", "),

		}
	}

	//recursion is pretty dumb pagination
	if len(*products.JSON200.Data) == limit && skip+limit < 250 { //fence post error
		page, err := g.GetIngredients(location, f, skip+limit)
		if err != nil {
			return nil, nil
		}
		ingredients = append(ingredients, page...)
	}

	return ingredients, nil
}

// toStr returns the string value if non-nil, or "empty" otherwise.
func toStr(s *string) string {
	if s == nil {
		return "empty"
	}
	return *s
}

// toFloat32 returns the float32 value if non-nil, or 0.0 otherwise.
func toFloat32(f *float32) float32 {
	if f == nil {
		return 0.0
	}
	return *f
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
