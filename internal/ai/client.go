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
	"strings"
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
	Price    string `json:"price"`    //TODO exclude empty
}

// IngredientData represents the ingredient data structure from the store
type IngredientData struct {
	Brand        *string  `json:"brand,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Size         *string  `json:"size,omitempty"`
	PriceRegular *float32 `json:"regularPrice,omitempty"`
	PriceSale    *float32 `json:"salePrice,omitempty"`
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

func (c *Client) GenerateRecipes(location *locations.Location, saleIngredients []IngredientData, instructions string, date time.Time, lastRecipes []string) (*ShoppingList, error) {
	messages := c.buildRecipeMessages(location, saleIngredients, instructions, date, lastRecipes)

	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	params := responses.ResponseNewParams{
		Model:        openai.ChatModelGPT5,
		Instructions: openai.String("You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines."),

		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
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

// encodeIngredientsToTOON converts ingredient data to TOON format
// TOON is a token-efficient format that uses tabular representation for uniform arrays
func encodeIngredientsToTOON(ingredients []IngredientData) string {
	if len(ingredients) == 0 {
		return "ingredients[0]:"
	}

	var result strings.Builder
	
	// Header: declare array length and field names in tabular format
	result.WriteString(fmt.Sprintf("ingredients[%d]{brand,description,size,regularPrice,salePrice}:\n", len(ingredients)))
	
	// Each ingredient as a row with comma-separated values
	for _, ing := range ingredients {
		brand := quoteIfNeeded(ptrToString(ing.Brand))
		description := quoteIfNeeded(ptrToString(ing.Description))
		size := quoteIfNeeded(ptrToString(ing.Size))
		regularPrice := floatToString(ing.PriceRegular)
		salePrice := floatToString(ing.PriceSale)
		
		result.WriteString(fmt.Sprintf("  %s,%s,%s,%s,%s\n", brand, description, size, regularPrice, salePrice))
	}
	
	return result.String()
}

// quoteIfNeeded quotes a string if it contains commas, colons, quotes, or looks like a special value
func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	
	// Check if quoting is needed
	needsQuotes := false
	
	// Check for special cases that require quotes
	if strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		needsQuotes = true
	}
	
	// Check for delimiters and special characters
	for _, char := range s {
		if char == ',' || char == ':' || char == '"' || char == '\\' || char == '\n' || char == '\r' || char == '\t' {
			needsQuotes = true
			break
		}
	}
	
	// Check if it looks like a boolean/number/null
	lower := strings.ToLower(s)
	if lower == "true" || lower == "false" || lower == "null" {
		needsQuotes = true
	}
	
	if !needsQuotes {
		return s
	}
	
	// Escape and quote
	escaped := strings.ReplaceAll(s, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\\r")
	escaped = strings.ReplaceAll(escaped, "\t", "\\t")
	
	return fmt.Sprintf(`"%s"`, escaped)
}

// ptrToString converts a string pointer to string, returning empty string if nil
func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// floatToString converts a float pointer to string, returning empty string if nil
func floatToString(f *float32) string {
	if f == nil {
		return ""
	}
	// Format without trailing zeros
	str := fmt.Sprintf("%.2f", *f)
	// Remove trailing zeros after decimal point
	str = strings.TrimRight(str, "0")
	str = strings.TrimRight(str, ".")
	return str
}

// buildRecipeMessages creates separate messages for the LLM to process more efficiently
func (c *Client) buildRecipeMessages(location *locations.Location, saleIngredients []IngredientData, instructions string, date time.Time, lastRecipes []string) responses.ResponseInputParam {
	var messages []responses.ResponseInputItemUnionParam

	// Message 1: System context and objective
	systemMessage := `# Objective
Generate 3 distinct, practical recipes using the provided constraints to maximize ingredient efficiency and meal variety.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch alongside protein is acceptable and satisfies the side requirement.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority) and that are in season for the current date and user's state location ` + date.Format("January 2nd") + ` in ` + location.State + `).
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Each recipe should serve 2 people.
- Provide clear, step-by-step instructions and an ingredient list for each recipe.
- Recipes should take under 1 hour to prepare, unless a special dish requires longer.
- Optionally include a wine pairing suggestion for each recipe if appropriate.
- Permitted cooking methods: oven, stove, grill, slow cooker.

# Output Format
- Each recipe includes:
  - Title
  - Description: Try to sell the dish and add some flair.
  - Ingredient list: should include quantities and price if in input.
  - Step-by-step instructions.
  - A guess at calorie count and healthiness
  - Optional wine or beer pairing.

# Planning & Verification
- Before generating each recipe, reference your checklist to ensure variety in cooking methods and cuisines, and confirm ingredient prioritization matches sale/seasonal data.`

	messages = append(messages, responses.ResponseInputItemParamOfMessage(systemMessage, responses.EasyInputMessageRoleSystem))

	// Message 2: Available ingredients (in TOON format for token efficiency)
	ingredientsMessage := `Ingredients currently on sale at local QFC/Fred Meyer (in TOON format - a token-efficient tabular format):

` + encodeIngredientsToTOON(saleIngredients)

	messages = append(messages, responses.ResponseInputItemParamOfMessage(ingredientsMessage, responses.EasyInputMessageRoleUser))

	// Message 3: Previous recipes to avoid (if any)
	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these from the past 2 weeks:\n")
		for _, recipe := range lastRecipes {
			prevRecipesMsg.WriteString(fmt.Sprintf("- %s\n", recipe))
		}
		messages = append(messages, responses.ResponseInputItemParamOfMessage(prevRecipesMsg.String(), responses.EasyInputMessageRoleUser))
	}

	// Message 4: Additional user instructions (if any)
	if instructions != "" {
		userInstructionsMsg := "Additional User Instructions:\n" + instructions
		messages = append(messages, responses.ResponseInputItemParamOfMessage(userInstructionsMsg, responses.EasyInputMessageRoleUser))
	}

	return messages
}
