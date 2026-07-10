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

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

const (
	gpt56Sol   = "gpt-5.6-sol"
	gpt56Terra = "gpt-5.6-terra"
	gpt56Luna  = "gpt-5.6-luna"

	defaultRecipeModel = gpt56Sol
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

func responseToRecipe(ctx context.Context, category, model string, resp *responses.Response) (*Recipe, error) {
	slog.InfoContext(ctx, "API usage", "ai_category", category, "model", model, responseUsageLogAttr(model, resp.Usage))
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

func (c *client) Regenerate(ctx context.Context, instructions []string, previousResponseID string) (*Recipe, error) {
	if previousResponseID == "" {
		return nil, fmt.Errorf("response ID is required for regeneration")
	}
	promptMessages := cleanInstructionMessages(instructions)
	messages := messagesToInput(promptMessages)

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

	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)
	return responseToRecipe(ctx, aiCategoryRecipe, c.model, resp)
}

func (c *client) GenerateRecipe(ctx context.Context, instructions []string, menuResponseID string) (*Recipe, error) {
	menuResponseID = strings.TrimSpace(menuResponseID)
	if menuResponseID == "" {
		return nil, fmt.Errorf("response ID is required for menu response generation")
	}
	promptMessages := cleanInstructionMessages(instructions)
	params := responses.ResponseNewParams{
		Model:              c.model,
		PreviousResponseID: openai.String(menuResponseID),
		// Previous response IDs do not carry over top-level instructions.
		Instructions: openai.String(systemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store: openai.Bool(true),
		Text:  scheme(c.recipeSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipe from menu response: %w", err)
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)

	return responseToRecipe(ctx, aiCategoryRecipe, c.model, resp)
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
	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryRecipeQuestion, "model", c.model, responseUsageLogAttr(c.model, resp.Usage))
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

func responseUsageLogAttr(model string, usage responses.ResponseUsage) slog.Attr {
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
		estimatedSpendLogAttr(estimateOpenAIResponseSpend(model, usage.InputTokens, usage.InputTokensDetails.CachedTokens, usage.OutputTokens)),
	)
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
