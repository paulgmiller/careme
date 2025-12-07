package ai

import (
	"careme/internal/kroger"
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

	"github.com/alpkeskin/gotoon"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/conversations"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
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

// todo collapse closer to
type Ingredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"` //should this and price be numbers? need units then
	Price    string `json:"price"`    //TODO exclude empty
}

type Recipe struct {
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Ingredients  []Ingredient `json:"ingredients"`
	Instructions []string     `json:"instructions"`
	Health       string       `json:"health"`
	DrinkPairing string       `json:"drink_pairing"`
	OriginHash   string       `json:"origin_hash"`
}

// ComputeHash calculates the SHA256 hash of the recipe content
func (r *Recipe) ComputeHash() string {
	// Exclude the Hash field itself from the hash computation
	jsonBytes := lo.Must(json.Marshal(r))
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

type ShoppingList struct {
	ConversationID string   `json:"conversation_id,omitempty" jsonschema:"-"`
	Recipes        []Recipe `json:"recipes" jsonschema:"required"`
}

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

const systemMessage = `
"You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines."

# Objective
Generate distinct, practical recipes using the provided constraints to maximize ingredient freshness, quality, and value while ensuring meal variety.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch alongside protein is acceptable and satisfies the side requirement.
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Provide clear, step-by-step instructions and an ingredient list for each recipe.
- Recipes should take under 1 hour to prepare, unless the user asks for something longer
- Optionally include a wine pairing suggestion for each recipe if appropriate. Suggest a local brand if possible.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority) 


# Output Format
- Each recipe includes:
  - Title
  - Description: Try to sell the dish and add some flair.
  - Ingredient list: should include quantities and price if in input.
  - Step-by-step instructions starting with prep. Don't prefix with numbers.
  - A guess at calorie count and healthiness
  - Optional wine or beer pairing.

# Planning & Verification
- Before generating each recipe, reference your checklist to ensure variety in cooking methods and cuisines, and confirm ingredient prioritization matches sale/seasonal data.`

func responseToShoppingList(ctx context.Context, resp *responses.Response) (*ShoppingList, error) {
	slog.InfoContext(ctx, "API usage", slog.Any("usage", json.RawMessage(resp.Usage.RawJSON())))
	var shoppingList ShoppingList
	if err := json.Unmarshal([]byte(resp.OutputText()), &shoppingList); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}
	if resp.Conversation.ID == "" {
		return nil, fmt.Errorf("failed to get conversation ID")
	}
	shoppingList.ConversationID = resp.Conversation.ID

	return &shoppingList, nil
}

func scheme(schema map[string]any) responses.ResponseTextConfigParam {
	return responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:   "recipes",
				Schema: schema, //https://platform.openai.com/docs/guides/structured-outputs?example=structured-data
			},
		},
	}
}

func (c *Client) Regenerate(ctx context.Context, newinstruction string, conversationID string) (*ShoppingList, error) {
	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	params := responses.ResponseNewParams{
		Model: openai.ChatModelGPT5_1,
		//only new input
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(newinstruction)},
		},
		Store: openai.Bool(true),
		Conversation: responses.ResponseNewParamsConversationUnion{
			OfString: openai.String(conversationID),
		},
		Text: scheme(c.schema),
	}
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes: %w", err)
	}

	return responseToShoppingList(ctx, resp)
}

// is this dependency on krorger unncessary? just pass in a blob of toml or whatever? same with last recipes?
func (c *Client) GenerateRecipes(ctx context.Context, location *locations.Location, saleIngredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (*ShoppingList, error) {
	messages, err := c.buildRecipeMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe messages: %w", err)
	}

	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	convo, err := client.Conversations.New(ctx, conversations.ConversationNewParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	params := responses.ResponseNewParams{
		Model:        openai.ChatModelGPT5_1,
		Instructions: openai.String(systemMessage),

		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
		},
		Store: openai.Bool(true),
		Conversation: responses.ResponseNewParamsConversationUnion{
			OfConversationObject: &responses.ResponseConversationParam{
				ID: convo.ID,
			},
		},
		Text: scheme(c.schema),
	}
	//should we stream. Can we pass past generation.

	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes: %w", err)
	}
	return responseToShoppingList(ctx, resp)
}

func user(msg string) responses.ResponseInputItemUnionParam {
	return responses.ResponseInputItemParamOfMessage(msg, responses.EasyInputMessageRoleUser)
}

// buildRecipeMessages creates separate messages for the LLM to process more efficiently
func (c *Client) buildRecipeMessages(location *locations.Location, saleIngredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (responses.ResponseInputParam, error) {
	var messages []responses.ResponseInputItemUnionParam
	//constants we might make variable later
	messages = append(messages, user("Each recipe should serve 2 people."))
	messages = append(messages, user("generate 3 recipes"))
	messages = append(messages, user("Permitted cooking methods: oven, stove, grill, slow cooker"))
	//location and date for seasonal ingredientss
	messages = append(messages, user("Prioritize ingredients that are in season for the current date and user's state location "+date.Format("January 2nd")+" in "+location.State+"."))

	//Available ingredients (in TOON format for token efficiency)
	ingredientsMessage := "Ingredients currently on sale in TOON format\n"

	encoded, err := gotoon.Encode(saleIngredients)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ingredients to TOON: %w", err)
	}
	ingredientsMessage += encoded

	messages = append(messages, user(ingredientsMessage))

	// Previous recipes to avoid (if any)
	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these from the past 2 weeks:\n")
		for _, recipe := range lastRecipes {
			prevRecipesMsg.WriteString(fmt.Sprintf("%s\n", recipe))
		}
		messages = append(messages, user(prevRecipesMsg.String()))
	}

	// Additional user instructions (if any)
	if instructions != "" {
		messages = append(messages, user(instructions))
	}

	return messages, nil
}
