package ai

import (
	"careme/internal/locations"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/responses"
	"github.com/samber/lo"

	"github.com/invopop/jsonschema"
)

type Client struct {
	provider   string
	apiKey     string
	model      string
	httpClient *http.Client
	schema     map[string]any
}

type Ingredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"` //should this and price be numbers? need units then
	Price    string `json:"price"`    //TODO exclue empty
}

type Recipe struct {
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Ingredients  []Ingredient `json:"ingredients"`
	Instructions []string     `json:"instructions"`
	Health       string       `json:"health"`
	DrinkPairing string       `json:"drink_pairing"`
}

// ComputeHash calculates the SHA256 hash of the recipe content
func (r *Recipe) ComputeHash() string {
	// Exclude the Hash field itself from the hash computation
	jsonBytes := lo.Must(json.Marshal(r))
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

type ShoppingList struct {
	Recipes []Recipe `json:"recipes"`
}

// Removed custom OpenAIRequest/OpenAIResponse in favor of official SDK types

func NewClient(provider, apiKey, model string) *Client {
	r := jsonschema.Reflector{
		DoNotReference: true, // no $defs and no $ref
		ExpandedStruct: true, // put the root type inline (not a $ref)
	}
	schema := r.Reflect(&ShoppingList{})
	schemaJSON, _ := json.Marshal(schema)
	var m map[string]any
	_ = json.Unmarshal(schemaJSON, &m)
	return &Client{
		provider:   provider,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
		schema:     m,
	}
}

func (c *Client) GenerateRecipes(location *locations.Location, saleIngredients []string, instructions string, date time.Time, lastRecipes []string) (*ShoppingList, error) {
	prompt := c.buildRecipePrompt(location, saleIngredients, instructions, date, lastRecipes)

	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	params := responses.ResponseNewParams{
		Model:        openai.ChatModelGPT5,
		Instructions: openai.String("You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines."),

		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(prompt), //TODO break this up seperate messages? What do we gain?
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   "recipes",
					Schema: c.schema, //https://platform.openai.com/docs/guides/structured-outputs?example=structured-data
				},
			},
		},

		//should we stream. Can we pass past generation.
	}

	resp, err := client.Responses.New(context.TODO(), params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes: %w", err)
	}
	// Parse the response to save recipes separately
	var shoppingList ShoppingList
	if err := json.Unmarshal([]byte(resp.OutputText()), &shoppingList); err != nil {
		slog.ErrorContext(context.TODO(), "failed to parse AI response", "error", err)
		// Fall back to saving the entire response as before
		return nil, err
	}

	return &shoppingList, nil
}

func (c *Client) buildRecipePrompt(location *locations.Location, saleIngredients []string, instructions string, date time.Time, lastRecipes []string) string {

	//TODO pull out meal count and people.
	//TODO json formatting
	//TODO store prompt in cache?
	// Place an overall combined ingredient summary as a ' < ul > ' at the bottom. Use a ' < table > ' for extra clarity if needed. Include name, quantity and sale prices.

	prompt := `# Objective
Generate 3 distinct, practical recipes using the provided constraints to maximize ingredient efficiency and meal variety while maintaining clear, user-friendly HTML output.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch alongside protein is acceptable and satisfies the side requirement.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority) and that are in season for the current date and user's state location ` + date.Format("January 2nd") + `  in ` + location.State + `).
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Each recipe should serve 2 people.
- Provide clear, step-by-step instructions and an ingredient list for each recipe.
- Recipes should take under 1 hour to prepare, unless a special dish requires longer.
- Optionally include a wine pairing suggestion for each recipe if appropriate.
- Permitted cooking methods: oven, stove, grill, slow cooker.


# Output Format
- Return all output json
- Each recipe includes:
  - Title
  - Description: Try to sell the dish and add some flair.
  - Ingredient list: should include quantities and price if in input.
  - Step-by-step instructions.
	- A guess at calorie count and healthiness
  - Optional wine or beer pairing.


# Verbosity
- Be concise but clear in step-by-step instructions and ingredient lists.

# Stop Conditions
- Only return output when 3 usable recipes or a user-friendly error message is generated.

# Planning & Verification
- Before generating each recipe, reference your checklist to ensure variety in cooking methods and cuisines, and confirm ingredient prioritization matches sale/seasonal data.

# Inputs`

	if len(saleIngredients) > 0 {
		prompt += "Ingredients currently on sale at local QFC/Fred Meyer:\n"
		for _, ingredient := range saleIngredients {
			prompt += fmt.Sprintf("- %s\n", ingredient)
		}
		prompt += "\n"
	}

	if prevRecipes := lastRecipes; len(prevRecipes) > 0 {
		prompt += "# Previous Recipes \n"
		prompt += "Avoid recipes similar to these from the past 2 weeks:\n"
		for _, recipe := range prevRecipes {
			prompt += fmt.Sprintf("- %s\n", recipe)
		}
		prompt += "\n"
	}

	prompt += `# Additional User Instructions:\n` + instructions + "\n"
	return prompt
}
