package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	locationtypes "careme/internal/locations/types"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

const recipePlanModel = defaultRecipeModel

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
	SideVegetable    string `json:"side_vegetable"`
	Fancy            bool   `json:"fancy"`
	// so generic this is directive, user instructions, servings, time? Split it up?
	RecipeInstructions []string `json:"recipe_instructions"`
}

func (p RecipePlan) Instructions() []string {
	instructions := []string{
		fmt.Sprintf("Cuisine direction for this recipe: %s.", p.Cuisine),
		fmt.Sprintf("Anchor ingredient direction for this recipe: %s.", p.AnchorIngredient),
		fmt.Sprintf("Suggested technique for this recipe: %s.", p.Technique),
		fmt.Sprintf("Side vegetable direction for this recipe: %s.", p.SideVegetable),
	}
	if p.Fancy {
		instructions = append(instructions, "This meal should be fancier, so it can be more expensive, longer, or richer.")
	}
	for _, instruction := range p.RecipeInstructions {
		if trimmed := strings.TrimSpace(instruction); trimmed != "" {
			instructions = append(instructions, "User direction for this recipe: "+trimmed)
		}
	}
	return instructions
}

// Notes about this list which is intended to force variety not be an all encompasing list.
// American, Chinese, Italian, French and mexican all have subcuisines
// Considering addign some way to look up "local cuisine" (Just add "local") to the list?
var cuisineList = []string{
	"Armenian",
	"Basque",
	"Burmese",
	"Cajun",
	"California Modern",
	"Caribbean",
	"Cantonese",
	"Chilean",
	"Chinese",
	"Creole",
	"Cuban",
	"Ethiopian",
	"Filipino",
	"French",
	"Georgian",
	"German",
	"Greek",
	"Indian",
	"Isan Thai",
	"Italian",
	"Jamaican",
	"Japanese",
	"Korean",
	"Lebanese",
	"Malaysian",
	"Mexican",
	"Moroccan",
	"New England",
	"North Indian",
	"Oaxacan",
	"Pacific Northwest",
	"Persian",
	"Peruvian",
	"Polish",
	"Provençal",
	"Senegalese",
	"Sichuan",
	"Sicilian",
	"South Indian",
	"Southern American",
	"Spanish",
	"Sri Lankan",
	"Tex-Mex",
	"Thai",
	"Tunisian",
	"Turkish",
	"Tuscan",
	"Vietnamese",
	"Yucatecan",
}

func pickN(xs []string, n int) []string {
	if n > len(xs) || n < 0 {
		panic("can't pick negative or more than we got")
	}
	xs = slices.Clone(xs)

	for i := range n {
		j := i + rand.N(len(xs)-i)
		xs[i], xs[j] = xs[j], xs[i]
	}

	return xs[:n]
}

const menuPlanSystemMessage = `
You are a menu planner for independent recipe generators.

Return compact planning labels, not recipes. Use short phrases, generally under 5 words, for cuisine, anchor_ingredient, side_vegetable, and technique. Set fancy to true only for the richer/splurgier/time intensive option.
Example plan: {"cuisine":"French Bistro","anchor_ingredient":"chicken thighs","technique":"braise","side_vegetable":"green beans","fancy":false,"recipe_instructions":["Use the user's anise in this recipe."]}
Try and ensure variety across cuisines, anchor ingredients, techniques, and side vegetables.
Choose anchor_ingredient and side_vegetable from the provided TSV ingredients. Use the exact ingredient Description text from the TSV. Do not choose an unavailable related ingredient; use the available ingredient's name instead.
Prioritize seasonal ingredients, sale value, practical weeknight cooking.
Assign user directions to recipe_instructions only for the specific recipe plans where they belong. If a user direction applies to every dish, repeat it in every recipe plan's recipe_instructions. If the user mentions having a limited ingredient without asking for it in every dish, assign it to only one fitting recipe.
Do not write recipe steps, prep instructions, shopping lists, rationale, or prose notes.`

func (c *client) CreateMenuPlan(ctx context.Context, location *locationtypes.Location, saleIngredients []InputIngredient,
	instructions []string, date time.Time, lastRecipes []string, count int,
) (*MenuPlan, error) {
	if count < 1 {
		return nil, fmt.Errorf("menu plan count must be greater than zero")
	}

	promptMessages, err := c.buildMenuPlanMessages(location, saleIngredients, instructions, date, lastRecipes, count)
	if err != nil {
		return nil, fmt.Errorf("failed to build menu plan messages: %w", err)
	}
	params := responses.ResponseNewParams{
		Model:        recipePlanModel,
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store: openai.Bool(true),
		Text:  scheme(c.menuSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)

	plan, err := responseToMenuPlan(ctx, aiCategoryMenu, recipePlanModel, resp)
	if err != nil {
		return nil, err
	}
	if err := alignMenuPlanIngredients(plan, saleIngredients); err != nil {
		slog.ErrorContext(ctx, "generated menu plan used unavailable ingredient", "error", err, "response_id", plan.ResponseID)
		return c.regenerateMenuPlanForIngredientMismatch(ctx, plan.ResponseID, saleIngredients, err, count)
	}
	return plan, nil
}

func (c *client) regenerateMenuPlanForIngredientMismatch(ctx context.Context, previousResponseID string, saleIngredients []InputIngredient, validationErr error, count int) (*MenuPlan, error) {
	feedback := fmt.Sprintf("The previous menu plan used an ingredient that was not available: %v. Regenerate the menu plan. Every anchor_ingredient and side_vegetable must exactly match a Description value from the ingredient TSV already provided.", validationErr)
	promptMessages := buildRegenerateMenuPlanMessages([]string{feedback}, count)
	params := responses.ResponseNewParams{
		Model:              recipePlanModel,
		PreviousResponseID: openai.String(previousResponseID),
		Instructions:       openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store: openai.Bool(true),
		Text:  scheme(c.menuSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate menu plan after ingredient mismatch: %w", err)
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)

	plan, err := responseToMenuPlan(ctx, aiCategoryMenu, recipePlanModel, resp)
	if err != nil {
		return nil, err
	}
	if err := alignMenuPlanIngredients(plan, saleIngredients); err != nil {
		return nil, fmt.Errorf("regenerated menu plan still used unavailable ingredient: %w", err)
	}
	return plan, nil
}

func (c *client) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*MenuPlan, error) {
	if previousResponseID == "" {
		return nil, fmt.Errorf("response ID is required for menu plan regeneration")
	}
	if count < 1 {
		return nil, fmt.Errorf("menu plan count must be greater than zero")
	}
	promptMessages := buildRegenerateMenuPlanMessages(instructions, count)

	params := responses.ResponseNewParams{
		Model:              recipePlanModel,
		PreviousResponseID: openai.String(previousResponseID),
		// Previous response IDs do not carry over top-level instructions.
		// https://developers.openai.com/api/docs/guides/text#message-roles-and-instruction-following
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store: openai.Bool(true),
		Text:  scheme(c.menuSchema),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate menu plan: %w", err)
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)
	return responseToMenuPlan(ctx, aiCategoryMenu, recipePlanModel, resp)
}

func responseToMenuPlan(ctx context.Context, category, model string, resp *responses.Response) (*MenuPlan, error) {
	var plan MenuPlan
	if err := json.Unmarshal([]byte(resp.OutputText()), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse variety plan: %w", err)
	}
	if strings.TrimSpace(resp.ID) == "" {
		return nil, fmt.Errorf("failed to get menu plan response ID")
	}
	plan.ResponseID = resp.ID
	slog.InfoContext(ctx, "generated menu plan", "ai_category", category, "model", model, "plan", lo.Must(json.Marshal(plan)), responseUsageLogAttr(model, resp.Usage))
	return &plan, nil
}

func alignMenuPlanIngredients(plan *MenuPlan, ingredients []InputIngredient) error {
	byDescription := make(map[string]bool, len(ingredients))
	for _, ingredient := range ingredients {
		description := strings.TrimSpace(ingredient.Description)
		if description == "" {
			continue
		}
		byDescription[normalizeMenuIngredientName(description)] = true
	}

	for i, plan := range plan.Plans {
		if err := alignMenuPlanIngredient(plan.AnchorIngredient, byDescription, "anchor_ingredient"); err != nil {
			return fmt.Errorf("plan %d: %w", i+1, err)
		}
		if err := alignMenuPlanIngredient(plan.SideVegetable, byDescription, "side_vegetable"); err != nil {
			return fmt.Errorf("plan %d: %w", i+1, err)
		}
	}
	return nil
}

func alignMenuPlanIngredient(label string, ingredients map[string]bool, field string) error {
	ok := ingredients[normalizeMenuIngredientName(label)]
	if !ok {
		return fmt.Errorf("%s %q is not an exact ingredient Description from the TSV", field, label)
	}
	return nil
}

func normalizeMenuIngredientName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func (c *client) buildMenuPlanMessages(location *locationtypes.Location, saleIngredients []InputIngredient,
	instructions []string, date time.Time, lastRecipes []string, count int,
) ([]PromptMessage, error) {
	var messages []PromptMessage
	messages = append(messages, userPromptMessage("Prioritize ingredients that are in season for the current date and user's state location "+date.Format("January 2nd")+" in "+location.State+"."))

	ingredientsMessage := fmt.Sprintf("%d ingredients available in TSV format with header.\n", len(saleIngredients))
	var buf strings.Builder
	if err := InputIngredientsToTSV(saleIngredients, &buf); err != nil {
		return nil, fmt.Errorf("failed to convert ingredients to TSV: %w", err)
	}
	ingredientsMessage += buf.String()
	messages = append(messages, userPromptMessage(ingredientsMessage))

	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Build %d distinct recipe plans by default. If the user's directions clearly ask for a different number of recipes, return that many plans instead. Keep the plan count between 1 and 6. Fit the available ingredients, seasonality, and price.", count)),
	)
	cuisines := pickN(cuisineList, 6)
	messages = append(messages, userPromptMessage("For extra variety, loosely draw from one of these cuisine styles if it fits the ingredients: "+strings.Join(cuisines, ", ")))
	// messages = append(messages, userPromptMessage("but don't overlook local cuisine"))

	// this fails on regen
	messages = append(messages, userPromptMessage("If doing more than 3 plans mark one plan fancy."))

	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these previously cooked:\n")
		for _, recipe := range lastRecipes {
			fmt.Fprintf(&prevRecipesMsg, "%s\n", recipe)
		}
		messages = append(messages, userPromptMessage(prevRecipesMsg.String()))
	}

	messages = append(messages, userPromptMessage("Default: cooking methods: oven, stove, grill, slow cooker"))
	messages = append(messages, userPromptMessage("Default: total recipe time, including prep and all timed steps, should stay under 1 hour"))
	messages = append(messages, userPromptMessage("Default: each recipe should serve 2 people."))
	messages = append(messages, cleanInstructionMessages(instructions)...)
	return messages, nil
}

func buildRegenerateMenuPlanMessages(instructions []string, count int) []PromptMessage {
	messages := cleanInstructionMessages(instructions)
	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Build %d replacement recipe plan(s) by default. If the user's directions clearly ask for a different number of recipes, return that many plans instead. Keep the plan count between 1 and 6. Avoid passed-on recipe titles and close variants. Fit the user's feedback.", count)),
	)
	messages = append(messages, userPromptMessage("If fancy plan was dismissed make one of the new ones fancy"))
	// messages = append(messages, userPromptMessage("Include one less-common cuisine direction."))

	return messages
}
