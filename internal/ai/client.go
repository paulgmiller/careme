package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	locationtypes "careme/internal/locations/types"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"

	"github.com/invopop/jsonschema"
)

type GeneratedImage struct {
	Body io.Reader
}

const (
	defaultRecipeModel = "gpt-5.5"
	defaultWineModel   = openai.ChatModelGPT5Mini
	recipePlanModel    = openai.ChatModelGPT5Mini // need to play witht this
)

// how close should this be to Input ingredint. Should we also add aisle or just echo productid so we can look it up
type Ingredient struct {
	ProductID   string `json:"id"`
	AisleNumber string `json:"aisle_number,omitempty" jsonschema:"-"`
	Name        string `json:"name"`
	Quantity    string `json:"quantity"` // amount used in the recipe, not the catalog package size
	Price       string `json:"price,omitempty" jsonschema:"-"`
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
	ResponseID   string       `json:"response_id,omitempty" jsonschema:"-"` // not in schema
	OriginHash   string       `json:"origin_hash,omitempty" jsonschema:"-"` // not in schema
	ParentHash   string       `json:"parent_hash,omitempty" jsonschema:"-"` // regeneration metadata, not in schema
	// Shove wine selection in here
}

// ComputeHash calculates the fnv128 hash of the recipe content
func (r *Recipe) ComputeHash() string {
	// OriginHash, ParentHash, Saved are intentionally excluded because they describe provenance or UI state,
	// not the recipe content itself. If ancestor links ever need to affect identity, that
	// is a separate model change and should not happen implicitly here.
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

// now we can reuse first recipes and people can go off in different directions.
// Mostly a collection of generaetd things could live in recipes instead of here.
type ShoppingList struct {
	Recipes []Recipe  `json:"recipes"`
	Plan    *MenuPlan `json:"plan"`
}

// question threads go off from the response that generated the recipe.
type QuestionResponse struct {
	Answer     string
	ResponseID string
}

type WineSelection struct {
	Wines      []Ingredient `json:"wines"`
	Commentary string       `json:"commentary"`
}

type client struct {
	recipeSchema map[string]any
	wineSchema   map[string]any
	menuSchema   map[string]any
	model        string
	wineModel    string
	oai          openai.Client
}

// ignoring model for now.
func NewClient(apiKey, _ string, httpClient *http.Client) *client {
	// ignor model for now.
	r := jsonschema.Reflector{
		DoNotReference: true, // no $defs and no $ref
		ExpandedStruct: true, // put the root type inline (not a $ref)
	}
	recipeSchema := r.Reflect(&Recipe{})
	recipeSchemaJSON, _ := json.Marshal(recipeSchema)
	wineSchema := r.Reflect(&WineSelection{})
	wineSchemaJSON, _ := json.Marshal(wineSchema)
	menuSchema := r.Reflect(&MenuPlan{})
	menuSchemaJson, _ := json.Marshal(menuSchema)
	var recipe map[string]any
	_ = json.Unmarshal(recipeSchemaJSON, &recipe)
	var wine map[string]any
	_ = json.Unmarshal(wineSchemaJSON, &wine)
	var menu map[string]any
	_ = json.Unmarshal(menuSchemaJson, &menu)

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	aiClient := openai.NewClient(opts...)
	return &client{
		oai:          aiClient,
		recipeSchema: recipe,
		wineSchema:   wine,
		menuSchema:   menu,
		model:        defaultRecipeModel,
		wineModel:    defaultWineModel,
	}
}

// edited out. Which recipe should be richer?!
const systemMessage = `
You are a professional chef and recipe developer helping working families cook varied weeknight dinners.

# Outcome
Create a practical, flavorful recipe using the provided sale ingredients, seasonal context, user preferences, recent-recipe history, cuisine and anchor ingredient.

# Recipe Requirements
- User instructions override defaults unless they make a recipe unsafe, uncookable, or impossible with the available ingredients.
- Unless the user asks for vegetarian or vegan food, include a protein plus at least one vegetable and/or starch.
- Include pastas, noodles, stir-fries, stews, braises, curries, casseroles, or other compositions when they fit the ingredients.
- Prioritize sale ingredients by value and quality. Only use prices from the input; never invent prices.
- Pantry items are allowed when common and inexpensive.
- Aim for healthy unless otherwise stated. Calorie estimates must be reasonable for the stated quantities and servings.
- Include wine pairing guidance when useful; otherwise explain briefly why a pairing is not needed.

# Field Guidance
- title: use a short, appetizing name.
- description: one appetizing sentence that notes what makes the dish practical, special, or seasonal.
- cook_time: provide the total elapsed recipe time such as "35 minutes"; include prep, cooking, resting, and any other timed instruction steps.
- cost_estimate: align the range with listed priced ingredients.
- ingredients: for catalog ingredients chosen from the TSV, set id to the exact ProductId. Leave id empty only for pantry items or ingredients not present in the TSV. Include the amount used in the recipe as quantity, not the catalog package size or sale size. Do not include prices; the app will add known store prices after generation.
- instructions: 5 to 8 clear steps; start with prep such as preheating, chopping, slicing, dicing, mixing, or make-ahead work before active cooking; do not rely on prep details from the ingredient list alone; end with plating; do not include prices; do not prefix steps with numbers.
- health: one short sentence with plausible calories and macro notes for the stated servings.
- drink_pairing: one concise sentence tied to the dish.
- wine_styles: at most two searchable consumer wine styles, such as "Pinot Noir" or "Sauvignon Blanc"; no regions, parenthetical notes, commas, "or", or "*-style blend" phrasing.

# Quality Checks
Before responding, ensure recipe is cookable, realistic, non-contradictory, correctly priced, safe, and visually appealing after plating.
Ensure cook_time reflects the total time implied by every instruction step, including prep, resting, and passive cooking time.
Do not include these checks in the output.`

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
Only choose wines present in the TSV. For each wine set id to the exact ProductId and include name and optionally quantity when useful.
Be creative not always the same safe picks. Consider the specific ingredients, cooking method, and flavor profile of the dish when making your selection.
Also for fancier/more expensive dishes consider more expensive wines.
`

const (
	recipeImageModel = "gpt-image-2" // dalle-3 is getting deprecated. 1.5 seems way better than 1.
	// WebP is materially smaller for these recipe photos on mobile, and GPT image models support direct WebP output.
	recipeImageOutputFormat = openai.ImageGenerateParamsOutputFormatWebP
	recipeImageQuality      = openai.ImageGenerateParamsQualityMedium
	recipeImageSize         = openai.ImageGenerateParamsSize1024x1024
)

func responseToRecipe(ctx context.Context, resp *responses.Response) (*Recipe, error) {
	slog.InfoContext(ctx, "API usage", responseUsageLogAttr(resp.Usage))
	var recipe Recipe
	if err := json.Unmarshal([]byte(resp.OutputText()), &recipe); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}
	recipe.WineStyles = normalizeRecipeWineStyles(recipe.WineStyles)
	if strings.TrimSpace(resp.ID) == "" {
		return nil, fmt.Errorf("failed to get response ID")
	}
	recipe.ResponseID = resp.ID
	return &recipe, nil
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

func (c *client) Regenerate(ctx context.Context, instructions []string, previousResponseID string) (*Recipe, error) {
	if previousResponseID == "" {
		return nil, fmt.Errorf("response ID is required for regeneration")
	}
	messages := cleanInstuctions(instructions)

	params := responses.ResponseNewParams{
		Model:              c.model,
		PreviousResponseID: openai.String(previousResponseID),
		// Previous response IDs do not carry over top-level instructions.
		// https://developers.openai.com/api/docs/guides/text#message-roles-and-instruction-following
		Instructions: openai.String(systemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
		},
		Store: openai.Bool(true),
		Text:  scheme(c.recipeSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes: %w", err)
	}

	return responseToRecipe(ctx, resp)
}

func (c *client) AskQuestion(ctx context.Context, question string, previousResponseID string) (*QuestionResponse, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}

	params := responses.ResponseNewParams{
		Model:        c.model,
		Instructions: openai.String("Answer the user's question about the recipe in plain text. Be concise and do not regenerate the full recipe or output JSON."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(question)},
		},
		Store: openai.Bool(true),
	}
	if previousResponseID != "" {
		params.PreviousResponseID = openai.String(previousResponseID)
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to answer question: %w", err)
	}
	answer := strings.TrimSpace(resp.OutputText())
	if answer == "" {
		return nil, fmt.Errorf("empty response from model")
	}
	if strings.TrimSpace(resp.ID) == "" {
		return nil, fmt.Errorf("failed to get response ID for question")
	}
	return &QuestionResponse{
		Answer:     answer,
		ResponseID: resp.ID,
	}, nil
}

func (c *client) GenerateRecipeImage(ctx context.Context, recipe Recipe) (*GeneratedImage, error) {
	prompt, err := buildRecipeImagePrompt(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe image prompt: %w", err)
	}

	resp, err := c.oai.Images.Generate(ctx, openai.ImageGenerateParams{
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

	slog.InfoContext(ctx, "API usage", imageUsageLogAttr(resp.Usage))
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

func responseUsageLogAttr(usage responses.ResponseUsage) slog.Attr {
	return slog.Group("usage",
		slog.Int64("inputTokens", usage.InputTokens),
		slog.Group("inputTokensDetails",
			slog.Int64("cachedTokens", usage.InputTokensDetails.CachedTokens),
		),
		slog.Int64("outputTokens", usage.OutputTokens),
		slog.Group("outputTokensDetails",
			slog.Int64("reasoningTokens", usage.OutputTokensDetails.ReasoningTokens),
		),
		slog.Int64("totalTokens", usage.TotalTokens),
	)
}

func imageUsageLogAttr(usage openai.ImagesResponseUsage) slog.Attr {
	return slog.Group("usage",
		slog.Int64("inputTokens", usage.InputTokens),
		slog.Group("inputTokensDetails",
			slog.Int64("imageTokens", usage.InputTokensDetails.ImageTokens),
			slog.Int64("textTokens", usage.InputTokensDetails.TextTokens),
		),
		slog.Int64("outputTokens", usage.OutputTokens),
		slog.Group("outputTokensDetails",
			slog.Int64("imageTokens", usage.OutputTokensDetails.ImageTokens),
			slog.Int64("textTokens", usage.OutputTokensDetails.TextTokens),
		),
		slog.Int64("totalTokens", usage.TotalTokens),
	)
}

func (c *client) PickWine(ctx context.Context, recipe Recipe, wines []InputIngredient) (*WineSelection, error) {
	prompt, err := buildWineSelectionPrompt(recipe, wines)
	if err != nil {
		return nil, fmt.Errorf("failed to build wine selection prompt: %w", err)
	}
	params := responses.ResponseNewParams{
		Model:        c.wineModel,
		Instructions: openai.String(winePrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(prompt)},
		},
		Text: scheme(c.wineSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to pick wine: %w", err)
	}

	var selection WineSelection
	if err := json.Unmarshal([]byte(resp.OutputText()), &selection); err != nil {
		return nil, fmt.Errorf("failed to parse wine selection: %w", err)
	}
	return &selection, nil
}

type MenuPlan struct {
	Plans      []RecipePlan `json:"plans"`
	ResponseID string       `json:"response_id,omitempty" jsonschema:"-"`
}

// meant for status
func (p MenuPlan) String() string {
	var sb strings.Builder
	sb.WriteString("Tenative Menu\n")
	for _, rp := range p.Plans {
		fmt.Fprintf(&sb, "%s using %s\n", rp.Cuisine, rp.AnchorIngredient)
	}
	return sb.String()
}

type RecipePlan struct {
	Cuisine          string `json:"cuisine"`
	AnchorIngredient string `json:"anchor_ingredient"`
	Technique        string `json:"technique"`
	Fancy            bool   `json:"fancy"`
}

func (p RecipePlan) Instructions() []string {
	instructions := []string{
		fmt.Sprintf("Cuisine direction for this recipe: %s.", p.Cuisine),
		fmt.Sprintf("Anchor ingredient direction for this recipe: %s.", p.AnchorIngredient),
		fmt.Sprintf("Suggested technique for this recipe: %s.", p.Technique),
	}
	if p.Fancy {
		instructions = append(instructions, "This meal should be fancier, so it can be more expensive, longer, or richer.")
	}
	return instructions
}

// Should we inject sample cuisines
// https://github.com/paulgmiller/careme/issues/449#issuecomment-4185138982
// const cusineList = "Italian, Japanese, Mexican, Cuban/Caribbean, Indian, Thai, French, Chinese, Spanish, Greek, Turkish, Ethiopian, Vietnamese, Korean, Moroccan, Peruvian"
const menuPlanSystemMessage = `
You are a menu planner for independent recipe generators.

Return compact planning labels, not recipes. Use short phrases, generally under 5 words, for cuisine, anchor_ingredient, and technique. Set fancy to true only for the richer/splurgier/time intensive option.
Example plan: {"cuisine":"French Bistro","anchor_ingredient":"chicken thighs","technique":"braise","fancy":false}
Try and ensure variety across cuisines, anchor ingredients, and techniques.
Prioritize seasonal ingredients, sale value, practical weeknight cooking. 
Do not write recipe steps, prep instructions, shopping lists, rationale, or prose notes.`

func (c *client) CreateMenuPlan(ctx context.Context, location *locationtypes.Location, saleIngredients []InputIngredient,
	instructions []string, date time.Time, lastRecipes []string, count int,
) (*MenuPlan, error) {
	if count < 1 {
		return nil, fmt.Errorf("menu plan count must be greater than zero")
	}

	prompt, err := c.buildMenuPlanMessages(location, saleIngredients, instructions, date, lastRecipes, count)
	if err != nil {
		return nil, fmt.Errorf("failed to build menu plan messages: %w", err)
	}
	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:        recipePlanModel,
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: prompt,
		},
		Store: openai.Bool(true),
		Text:  scheme(c.menuSchema),
	})
	if err != nil {
		return nil, err
	}

	return responseToMenuPlan(ctx, resp)
}

func (c *client) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*MenuPlan, error) {
	if previousResponseID == "" {
		return nil, fmt.Errorf("response ID is required for menu plan regeneration")
	}
	if count < 1 {
		return nil, fmt.Errorf("menu plan count must be greater than zero")
	}
	messages := buildRegenerateMenuPlanMessages(instructions, count)

	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:              recipePlanModel,
		PreviousResponseID: openai.String(previousResponseID),
		// Previous response IDs do not carry over top-level instructions.
		// https://developers.openai.com/api/docs/guides/text#message-roles-and-instruction-following
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
		},
		Store: openai.Bool(true),
		Text:  scheme(c.menuSchema),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate menu plan: %w", err)
	}
	return responseToMenuPlan(ctx, resp)
}

func responseToMenuPlan(ctx context.Context, resp *responses.Response) (*MenuPlan, error) {
	var plan MenuPlan
	if err := json.Unmarshal([]byte(resp.OutputText()), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse variety plan: %w", err)
	}
	if strings.TrimSpace(resp.ID) == "" {
		return nil, fmt.Errorf("failed to get menu plan response ID")
	}
	plan.ResponseID = resp.ID
	slog.InfoContext(ctx, "generated menu plan", "plan", lo.Must(json.Marshal(plan)))
	return &plan, nil
}

func (c *client) buildMenuPlanMessages(location *locationtypes.Location, saleIngredients []InputIngredient,
	instructions []string, date time.Time, lastRecipes []string, count int,
) ([]responses.ResponseInputItemUnionParam, error) {
	messages, err := c.buildRecipeContextMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, err
	}
	messages = append(messages,
		user(fmt.Sprintf("Build exactly %d distinct recipe plans that fit the available ingredients, seasonality, and price.", count)),
	)
	if count >= 3 {
		messages = append(messages, user("Mark one plan fancy."))
		messages = append(messages, user("Include one less-common cuisine direction."))
	}
	return messages, nil
}

func buildRegenerateMenuPlanMessages(instructions []string, count int) []responses.ResponseInputItemUnionParam {
	messages := cleanInstuctions(instructions)
	messages = append(messages,
		user(fmt.Sprintf("Pick exactly %d replacement plans. Avoid passed-on recipe titles and close variants. Fit the user's feedback.", count)),
	)
	// ideally do this if they dismissed fancy.
	if count >= 3 {
		messages = append(messages, user("Mark one replacement plan fancy."))
		messages = append(messages, user("Include one less-common cuisine direction."))
	}
	return messages
}

func (c *client) GenerateRecipe(ctx context.Context, location *locationtypes.Location, saleIngredients []InputIngredient,
	instructions []string, date time.Time, lastRecipes []string,
) (*Recipe, error) {
	messages, err := c.buildRecipeMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe messages: %w", err)
	}

	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:        c.model,
		Instructions: openai.String(systemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messages,
		},
		Store: openai.Bool(true),
		Text:  scheme(c.recipeSchema),
	})
	if err != nil {
		return nil, err
	}

	return responseToRecipe(ctx, resp)
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
func buildWineSelectionPrompt(recipe Recipe, wines []InputIngredient) (string, error) {
	var wineTSV strings.Builder
	if err := InputIngredientsToTSV(wines, &wineTSV); err != nil {
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
func (c *client) buildRecipeMessages(location *locationtypes.Location, saleIngredients []InputIngredient, instructions []string, date time.Time, lastRecipes []string) ([]responses.ResponseInputItemUnionParam, error) {
	messages, err := c.buildRecipeContextMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, err
	}
	messages = append(messages, user("Default: each recipe should serve 2 people."))
	return messages, nil
}

func (c *client) buildRecipeContextMessages(location *locationtypes.Location, saleIngredients []InputIngredient, instructions []string, date time.Time, lastRecipes []string) ([]responses.ResponseInputItemUnionParam, error) {
	var messages []responses.ResponseInputItemUnionParam
	// constants we might make variable later
	messages = append(messages, user("Prioritize ingredients that are in season for the current date and user's state location "+date.Format("January 2nd")+" in "+location.State+"."))
	messages = append(messages, user("Default: total recipe time, including prep and all timed steps, should stay under 1 hour"))
	messages = append(messages, user("Default: cooking methods: oven, stove, grill, slow cooker"))

	// todo reuse context via response id?
	ingredientsMessage := fmt.Sprintf("%d ingredients available in TSV format with header.\n", len(saleIngredients))
	var buf strings.Builder
	if err := InputIngredientsToTSV(saleIngredients, &buf); err != nil {
		return nil, fmt.Errorf("failed to convert ingredients to TSV: %w", err)
	}
	ingredientsMessage += buf.String()
	messages = append(messages, user(ingredientsMessage))

	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these previously cooked:\n")
		for _, recipe := range lastRecipes {
			fmt.Fprintf(&prevRecipesMsg, "%s\n", recipe)
		}
		messages = append(messages, user(prevRecipesMsg.String()))
	}

	return append(messages, cleanInstuctions(instructions)...), nil
}

func (c *client) Ready(ctx context.Context) error {
	// more CORRECT to do a very simple response request with allowed tokens 1 but this seems cheaper
	// https://chatgpt.com/share/6984da16-ff88-8009-8486-4e0479ac6a01
	// could only do it once to ensure startup
	_, err := c.oai.Models.List(ctx)
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
