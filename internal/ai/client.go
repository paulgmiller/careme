package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"time"

	"careme/internal/kroger"
	"careme/internal/locations"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/conversations"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"

	"github.com/invopop/jsonschema"
)

type Client struct {
	apiKey     string
	schema     map[string]any
	wineSchema map[string]any
	model      string
}

// todo collapse closer to
type Ingredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"` // should this and price be numbers? need units then
	Price    string `json:"price"`    // TODO exclude empty
}

type Recipe struct {
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	CookTime     string       `json:"cook_time"`
	CostEstimate string       `json:"cost_estimate"`
	Ingredients  []Ingredient `json:"ingredients"`
	Instructions []string     `json:"instructions"`
	Health       string       `json:"health"`
	DrinkPairing string       `json:"drink_pairing"`
	WineStyles   []string     `json:"wine_styles"`
	OriginHash   string       `json:"origin_hash,omitempty" jsonschema:"-"`      // not in schema
	Saved        bool         `json:"previously_saved,omitempty" jsonschema:"-"` // not in schema
}

// ComputeHash calculates the fnv128 hash of the recipe content
func (r *Recipe) ComputeHash() string {
	//these are intentionally dropped as they don't change the content and are metadata
	// maybe they should have always been outside the struct.
	/// OriginHash = ""
	// Saved = false
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, r.Title))
	lo.Must(io.WriteString(fnv, r.Description))
	lo.Must(io.WriteString(fnv, r.CookTime))
	lo.Must(io.WriteString(fnv, r.CostEstimate))
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

// intionally not including ConversationID to preserve old hashes
type ShoppingList struct {
	ConversationID string   `json:"conversation_id,omitempty" jsonschema:"-"`
	Recipes        []Recipe `json:"recipes" jsonschema:"required"`
}

type WineSelection struct {
	Wines      []Ingredient `json:"wines"`
	Commentary string       `json:"commentary"`
}

// ignoring model for now.
func NewClient(apiKey, _ string) *Client {
	// ignor model for now.
	r := jsonschema.Reflector{
		DoNotReference: true, // no $defs and no $ref
		ExpandedStruct: true, // put the root type inline (not a $ref)
	}
	recipesSchema := r.Reflect(&ShoppingList{})
	recipesSchemaJSON, _ := json.Marshal(recipesSchema)
	wineSchema := r.Reflect(&WineSelection{})
	wineSchemaJSON, _ := json.Marshal(wineSchema)
	var m map[string]any
	_ = json.Unmarshal(recipesSchemaJSON, &m)
	var wine map[string]any
	_ = json.Unmarshal(wineSchemaJSON, &wine)
	return &Client{
		apiKey:     apiKey,
		schema:     m,
		wineSchema: wine,
		model:      openai.ChatModelGPT5_4,
	}
}

const systemMessage = `
"You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines."

# Objective
Generate distinct, practical recipes using the provided constraints to maximize ingredient freshness, quality, and value while ensuring meal variety.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch is also good.
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Provide clear, step-by-step instructions and an ingredient list for each recipe. repeat amounts and prep for each recipe in instructions.
- Optionally include a wine pairing suggestion for each recipe if appropriate. Suggest a couple of styles. Really put your Sommielier hat on for this.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority but but don't pay more for something on sale than a similar ingredient that isn't)


# Output Format
- Each recipe includes:
  - title: A short catchy name for the dish.
  - description: Try to sell the dish and add some flair.
  - cook_time: Estimated cook time (for example: "35 minutes")
  - cost_estimate: Estimated total cost in dollars (for example: "$18-24")
  - instructions: should include quantities and price if in input.
  - Step-by-step instructions starting with prep. Don't prefix with numbers.
  - health: Estimated Calorie count and other nutrient health tips.
  - drink_pairing: the wine pairing suggestion mentioned in instructions
  - wine_styles: Two or fewer consumer-recognizable wine styles for search (for example: "Pinot Noir", "Sauvignon Blanc", "Cabernet Sauvignon").
  - wine_styles must only contain searchable style names: no regions, no parenthetical notes, no commas, no "or", no "*-style blend" phrasing.

# Planning & Verification
- Reference your checklist to ensure variety in cooking methods and cuisines
- confirm ingredient prioritization matches sale/seasonal data.
- Check cooktime and cost match instructions and ingredient list`

func responseToShoppingList(ctx context.Context, resp *responses.Response) (*ShoppingList, error) {
	slog.InfoContext(ctx, "API usage", slog.Any("usage", json.RawMessage(resp.Usage.RawJSON())))
	var shoppingList ShoppingList
	if err := json.Unmarshal([]byte(resp.OutputText()), &shoppingList); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}
	normalizeWineStyles(&shoppingList)
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
				Schema: schema, // https://platform.openai.com/docs/guides/structured-outputs?example=structured-data
			},
		},
	}
}

func (c *Client) Regenerate(ctx context.Context, instructions []string, conversationID string) (*ShoppingList, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation ID is required for regeneration")
	}
	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	messages := cleanInstuctions(instructions)

	params := responses.ResponseNewParams{
		Model: c.model,
		// only new input
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
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

func (c *Client) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", fmt.Errorf("question is required")
	}
	if conversationID == "" {
		return "", fmt.Errorf("conversation ID is required for questions")
	}
	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	params := responses.ResponseNewParams{
		Model:        c.model,
		Instructions: openai.String("Answer the user's question about the recipe in plain text. Be concise and do not regenerate the full recipe or output JSON."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(question)},
		},
		Store: openai.Bool(true),
		Conversation: responses.ResponseNewParamsConversationUnion{
			OfString: openai.String(conversationID),
		},
	}
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to answer question: %w", err)
	}
	answer := strings.TrimSpace(resp.OutputText())
	if answer == "" {
		return "", fmt.Errorf("empty response from model")
	}
	return answer, nil
}

func (c *Client) PickWine(ctx context.Context, conversationID string, recipeTitle string, wines []kroger.Ingredient) (*WineSelection, error) {
	conversationID = strings.TrimSpace(conversationID)
	recipeTitle = strings.TrimSpace(recipeTitle)
	if conversationID == "" {
		return nil, fmt.Errorf("conversation ID is required for wine picks")
	}
	if recipeTitle == "" {
		return nil, fmt.Errorf("recipe title is required for wine picks")
	}
	if len(wines) == 0 {
		return nil, fmt.Errorf("wines are required for wine picks")
	}
	var wineTSV strings.Builder
	err := kroger.ToTSV(wines, &wineTSV)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to convert wines to TSV", "error", err)
		return nil, err
	}
	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	input := []responses.ResponseInputItemUnionParam{user(fmt.Sprintf("Candidate wines:\n%s", wineTSV.String()))}
	params := responses.ResponseNewParams{
		Model: c.model,
		Instructions: openai.String(
			"Act as a sommelier. Select 1 to 2 wines from the provided TSV that pair well with the recipe " + recipeTitle + "." +
				"Return JSON with wines (ingredient array) and commentary about why those particular wines work well" +
				"Pick wine sizes appropriate to number of people. Price according to the meal fanciness" +
				"For each wine include name and optionally quantity/price when available from TSV.",
		),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Store: openai.Bool(true),
		Conversation: responses.ResponseNewParamsConversationUnion{
			OfString: openai.String(conversationID),
		},
		Text: scheme(c.wineSchema),
	}
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to pick wine: %w", err)
	}

	var selection WineSelection
	if err := json.Unmarshal([]byte(resp.OutputText()), &selection); err != nil {
		return nil, fmt.Errorf("failed to parse wine selection: %w", err)
	}
	return &selection, nil
}

// is this dependency on krorger unncessary? just pass in a blob of toml or whatever? same with last recipes?
func (c *Client) GenerateRecipes(ctx context.Context, location *locations.Location, saleIngredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ShoppingList, error) {
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
	// should we stream. Can we pass past generation.

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
func (c *Client) buildRecipeMessages(location *locations.Location, saleIngredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (responses.ResponseInputParam, error) {
	var messages []responses.ResponseInputItemUnionParam
	// constants we might make variable later
	messages = append(messages, user("Prioritize ingredients that are in season for the current date and user's state location "+date.Format("January 2nd")+" in "+location.State+"."))
	messages = append(messages, user("Default: each recipe should serve 2 people."))
	messages = append(messages, user("Default: generate 3 recipes"))
	messages = append(messages, user("Default: prep and cook time under 1 hour"))
	messages = append(messages, user("Default: cooking methods: oven, stove, grill, slow cooker"))

	ingredientsMessage := fmt.Sprintf("%d ingredients available in TSV format with header.\n", len(saleIngredients))
	var buf strings.Builder
	if err := kroger.ToTSV(saleIngredients, &buf); err != nil {
		return nil, fmt.Errorf("failed to convert ingredients to TSV: %w", err)
	}
	ingredientsMessage += buf.String()
	messages = append(messages, user(ingredientsMessage))

	// Previously cooked recipes to avoid (if any).
	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these previously cooked:\n")
		for _, recipe := range lastRecipes {
			fmt.Fprintf(&prevRecipesMsg, "%s\n", recipe)
		}
		messages = append(messages, user(prevRecipesMsg.String()))
	}

	// Additional user instructions (if any)

	messages = append(messages, cleanInstuctions(instructions)...)

	return messages, nil
}

func (c *Client) Ready(ctx context.Context) error {
	// more CORRECT to do a very simple response request with allowed tokens 1 but this seems cheaper
	// https://chatgpt.com/share/6984da16-ff88-8009-8486-4e0479ac6a01
	// could only do it once to ensure startup
	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	_, err := client.Models.List(ctx)
	return err
}

func cleanInstuctions(instructions []string) []responses.ResponseInputItemUnionParam {
	var responses []responses.ResponseInputItemUnionParam
	for _, i := range instructions {
		i = strings.TrimSpace(i)
		if i == "" {
			continue
		}
		responses = append(responses, user(i))
	}
	return responses
}

func normalizeWineStyles(shoppingList *ShoppingList) {
	if shoppingList == nil {
		return
	}
	for i := range shoppingList.Recipes {
		shoppingList.Recipes[i].WineStyles = normalizeRecipeWineStyles(shoppingList.Recipes[i].WineStyles)
	}
}

func normalizeRecipeWineStyles(styles []string) []string {
	if len(styles) == 0 {
		return nil
	}
	cleaned := make([]string, 0, min(len(styles), 2))
	seen := map[string]struct{}{}
	for _, style := range styles {
		normalized := normalizeWineStyle(style)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, normalized)
		if len(cleaned) == 2 {
			break
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func normalizeWineStyle(style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return ""
	}
	if idx := strings.IndexAny(style, "(["); idx >= 0 {
		style = strings.TrimSpace(style[:idx])
	}
	style = strings.TrimSpace(strings.TrimSuffix(style, "."))
	if style == "" {
		return ""
	}
	return strings.Join(strings.Fields(style), " ")
}
