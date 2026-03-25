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
	wineModel  string
}

type GeneratedImage struct {
	Body io.Reader
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
		wineModel:  openai.ChatModelGPT5Mini,
	}
}

const systemMessage = `
You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines.

# Objective
Generate distinct, practical recipes using the provided constraints to maximize ingredient freshness, quality, and value while ensuring meal variety.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. Include pastas, noodles, stir fry's, stews, braises, curries, casserole and other compositions.
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Provide clear, step-by-step instructions and an ingredient list for each recipe. Repeat amounts and prep for each recipe in instructions. Details on how ingredients are cut and plated.
- include a optional wine pairing suggestion for each recipe if appropriate. Suggest a couple of styles. Really put your Sommielier hat on for this.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority but be willing to pay for better ingredients). Only use prices given don't invent prices.
- Aim for healthy unless otherwise stated. Calorie estimates must be reasonable for the stated ingredient quantities and servings.
- Aim for an aesthetically and texturally pleasing dish. Think about color (not too monochrome), texture (not all mushy), and plating (how would a restaurant plate this?).
- Suggest at least one recipe that is a little bit richer in terms of price, calories or prep time, be sure to mention in description.
- Suggest at least one recipe that is different ethnic cuisine.

# Output Format
- List of recipe each includes:
  - title: A short catchy name for the dish.
  - description: Try to sell the dish and add some flair.
  - cook_time: Estimated cook time (for example: "35 minutes")
  - cost_estimate: Estimated total cost in dollars (for example: "$18-24")
  - ingredients:  should include quantities and price if in input. Can include widely availble pantry items not explicitly listed in user input.
  - instructions: Step-by-step starting with prep and ending with plating. Don't prefix with numbers.
  - health: Estimated Calorie count and other macro nutrient details.
  - drink_pairing: the wine pairing suggestion mentioned in instructions
  - wine_styles: Two or fewer consumer-recognizable wine styles for search (for example: "Pinot Noir", "Sauvignon Blanc", "Cabernet Sauvignon"). Must only contain searchable style names: no regions, no parenthetical notes, no commas, no "or", no "*-style blend" phrasing.

# Planning & Verification
- Reference your checklist to ensure variety in cooking methods and cuisines
- Confirm ingredient prioritization matches sale/seasonal data.
- Verify every ingredient line with a price uses the same product and price from input data.
- Recalculate cost estimate from listed priced ingredients and ensure it aligns with cost_estimate.
- Double-check calorie guidance in health against the ingredient list and portion size.
- Read each instruction step in order and ensure the flow is realistic, non-contradictory, and fully cookable
- Verify the liquids reduce in stated time
- Verify technical terms are used correctly.
- Verify the dish have a good appearance after plating`

const recipeImagePromptInstructions = `
Generate a realistic overhead food photograph of a single finished plate.
- Home cooked by a above average cook, not a restaurant or food stylist.
- Keep plating simple and believable. No tweezers, foam, edible flowers, microgreens, or luxury flourishes unless in recipe instructions.
- Use a simple kitchen counter, stovetop, sheet pan, wooden table, or casual dining table backdrop.
- Use natural colors, ordinary cookware or tableware, and realistic portions 
- Avoid text, labels, branded packaging, people, hands, collages, and extra side dishes 
- If the recipe has multiple components, show them plated together
`

const winePrompt = `
Act as a sommelier for the recipe provided below
Select 1 to 2 wines from the provided TSV that best match the dish
Return JSON with wines (ingredient array) and concise commentary explaining why those specific bottles work.
Only choose wines present in the TSV. For each wine include name and optionally quantity/price when available from TSV.
Be creative not always the same safe picks. Consider the specific ingredients, cooking method, and flavor profile of the dish when making your selection.
Also for fancier/more expensive dishes consider more expensive wines.
`

const (
	recipeImageModel = openai.ImageModelGPTImage1_5 // dalle-3 is getting deprecated. 1.5 seems way better than 1.
	// WebP is materially smaller for these recipe photos on mobile, and GPT image models support direct WebP output.
	recipeImageOutputFormat = openai.ImageGenerateParamsOutputFormatWebP
	recipeImageQuality      = openai.ImageGenerateParamsQualityHigh
	recipeImageSize         = openai.ImageGenerateParamsSize1024x1024
)

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

func (c *Client) GenerateRecipeImage(ctx context.Context, recipe Recipe) (*GeneratedImage, error) {
	prompt, err := buildRecipeImagePrompt(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe image prompt: %w", err)
	}

	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	resp, err := client.Images.Generate(ctx, openai.ImageGenerateParams{
		Prompt:       prompt,
		Model:        recipeImageModel,
		N:            openai.Int(1),
		OutputFormat: recipeImageOutputFormat,
		Quality:      recipeImageQuality,
		Size:         recipeImageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipe image: %w", err)
	}

	slog.InfoContext(ctx, "API usage", slog.Any("usage", json.RawMessage(resp.Usage.RawJSON())))
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("image generation returned no images")
	}
	imageBody := strings.TrimSpace(resp.Data[0].B64JSON)
	if imageBody == "" {
		return nil, fmt.Errorf("image generation returned empty image data")
	}

	return &GeneratedImage{
		Body: base64.NewDecoder(base64.StdEncoding, strings.NewReader(imageBody)),
	}, nil
}

func (c *Client) PickWine(ctx context.Context, recipe Recipe, wines []kroger.Ingredient) (*WineSelection, error) {
	prompt, err := buildWineSelectionPrompt(recipe, wines)
	if err != nil {
		return nil, fmt.Errorf("failed to build wine selection prompt: %w", err)
	}
	client := openai.NewClient(option.WithAPIKey(c.apiKey))
	params := responses.ResponseNewParams{
		Model:        c.wineModel,
		Instructions: openai.String(winePrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(prompt)},
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

func buildRecipeImagePrompt(recipe Recipe) (string, error) {
	var promptBuilder strings.Builder
	fmt.Fprintf(&promptBuilder, "%s\n", recipeImagePromptInstructions)
	fmt.Fprintf(&promptBuilder, "\n")
	fmt.Fprintf(&promptBuilder, "Recipe:\n")
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Title)
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Description)
	fmt.Fprintf(&promptBuilder, "Instructions:\n")
	for _, ins := range recipe.Instructions {
		fmt.Fprintf(&promptBuilder, "- %s\n", ins)
	}
	return promptBuilder.String(), nil
}

// similiar to image generation builder
func buildWineSelectionPrompt(recipe Recipe, wines []kroger.Ingredient) (string, error) {
	var wineTSV strings.Builder
	if err := kroger.ToTSV(wines, &wineTSV); err != nil {
		return "", fmt.Errorf("failed to convert wines to TSV: %w", err)
	}

	var promptBuilder strings.Builder
	fmt.Fprintf(&promptBuilder, "Recipe:\n")
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Title)
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Description)
	fmt.Fprintf(&promptBuilder, "Instructions:\n")
	for _, ins := range recipe.Instructions {
		fmt.Fprintf(&promptBuilder, "- %s\n", ins)
	}
	fmt.Fprintf(&promptBuilder, "Existing drink pairing note: %s\n", recipe.DrinkPairing)
	// add cost estimate when we believ it?
	fmt.Fprintf(&promptBuilder, "\nCandidate wines TSV:\n%s", wineTSV.String())
	return promptBuilder.String(), nil
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
