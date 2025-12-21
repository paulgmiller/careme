package ai

import (
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
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
	apiKey string
	schema map[string]any
	model  string
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
	OriginHash   string       `json:"origin_hash,omitempty" jsonschema:"-"`      //not in schema
	Saved        bool         `json:"previously_saved,omitempty" jsonschema:"-"` //not in schema
}

// ComputeHash calculates the fnv128 hash of the recipe content
func (r *Recipe) ComputeHash() string {
	//these aer intntioanlly dropped.
	/// OriginHash = ""
	// Saved = false
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, r.Title))
	lo.Must(io.WriteString(fnv, r.Description))
	for _, ing := range r.Ingredients {
		lo.Must(io.WriteString(fnv, ing.Name))
		lo.Must(io.WriteString(fnv, ing.Quantity))
		lo.Must(io.WriteString(fnv, ing.Price))
	}
	for _, instr := range r.Instructions {
		lo.Must(io.WriteString(fnv, instr))
	}
	lo.Must(io.WriteString(fnv, r.Health))
	lo.Must(io.WriteString(fnv, r.DrinkPairing))
	return base64.URLEncoding.EncodeToString(fnv.Sum(nil))
}

//intionally not including ConversationID to preserve old hashes

type ShoppingList struct {
	ConversationID string   `json:"conversation_id,omitempty" jsonschema:"-"`
	Recipes        []Recipe `json:"recipes" jsonschema:"required"`
}

// ignoring model for now.
func NewClient(apiKey, _ string) *Client {
	//ignor model for now.
	r := jsonschema.Reflector{
		DoNotReference: true, // no $defs and no $ref
		ExpandedStruct: true, // put the root type inline (not a $ref)
	}
	schema := r.Reflect(&ShoppingList{})
	schemaJSON, _ := json.Marshal(schema)
	var m map[string]any
	_ = json.Unmarshal(schemaJSON, &m)
	return &Client{
		apiKey: apiKey,
		schema: m,
		model:  openai.ChatModelGPT5_2,
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

func (c *Client) Regenerate(ctx context.Context, newInstruction string, conversationID string) (*ShoppingList, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation ID is required for regeneration")
	}
	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	params := responses.ResponseNewParams{
		Model: c.model,
		//only new input
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(newInstruction)},
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
		Model:        c.model,
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
