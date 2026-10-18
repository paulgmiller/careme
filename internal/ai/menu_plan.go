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

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

type MenuPlan struct {
	Plans              []RecipePlan `json:"plans" yaml:"plans"`
	ChefNoteSuggestion string       `json:"chef_note_suggestion" yaml:"chef_note_suggestion"`
	ResponseID         string       `json:"response_id,omitempty" yaml:"response_id,omitempty" jsonschema:"-"`
	PromptCacheKey     string       `json:"prompt_cache_key,omitempty" yaml:"prompt_cache_key,omitempty" jsonschema:"-"`
}

func (p MenuPlan) ResponseRef() ResponseRef {
	return ResponseRef{ID: p.ResponseID, PromptCacheKey: p.PromptCacheKey}
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

// MealCategory is a recipe's primary direction within a varied menu.
type MealCategory string

const (
	MealCategoryHealthy   MealCategory = "healthy"
	MealCategoryFancy     MealCategory = "fancy"
	MealCategoryLocal     MealCategory = "local"
	MealCategoryDifferent MealCategory = "different"
)

func (MealCategory) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Enum: []any{
		string(MealCategoryHealthy), string(MealCategoryFancy), string(MealCategoryLocal), string(MealCategoryDifferent),
	}}
}

func (c MealCategory) instruction() string {
	switch c {
	case MealCategoryHealthy:
		return "Healthy: emphasize ancient or whole grains, more vegetables, lighter proteins and preparations."
	case MealCategoryFancy:
		return "Fancy: make this meal fancier with richer flavors, more indulgent, higher-calorie ingredients, greater expense, and slower cooking; it may exceed the default one-hour limit."
	case MealCategoryLocal:
		return "Local: emphasize produce and proteins associated with the user's region and current season, using available ingredients; do not claim verified local sourcing."
	case MealCategoryDifferent:
		return "Different: emphasize lesser-known cuisines, regional dishes, and less-familiar seasonings."
	default:
		return ""
	}
}

const categoryDistributionMessage = "Spread meals across healthy, fancy, local, and different categories. For up to four meals, choose distinct categories that fit the ingredients and user requests; for larger menus, cover all four before repeating. Explicit user requests take priority over category variety."

type RecipePlan struct {
	Cuisine          string       `json:"cuisine" yaml:"cuisine"`
	AnchorIngredient string       `json:"anchor_ingredient" yaml:"anchor_ingredient"`
	DishFormat       string       `json:"dish_format" yaml:"dish_format"`
	SideVegetable    string       `json:"side_vegetable" yaml:"side_vegetable"`
	Category         MealCategory `json:"category" yaml:"category"`
	// so generic this is directive, user instructions, servings, time? Split it up?
	RecipeInstructions []string `json:"recipe_instructions" yaml:"recipe_instructions,omitempty"`
}

func (p RecipePlan) Instructions() []string {
	instructions := []string{
		fmt.Sprintf("Cuisine direction for this recipe: %s.", p.Cuisine),
		fmt.Sprintf("Anchor ingredient direction for this recipe: %s.", p.AnchorIngredient),
		fmt.Sprintf("Suggested dish format for this recipe: %s.", p.DishFormat),
		fmt.Sprintf("Side vegetable direction for this recipe: %s.", p.SideVegetable),
	}
	if direction := p.Category.instruction(); direction != "" {
		instructions = append(instructions, direction+" Explicit user dietary, time, budget, and equipment constraints still apply.")
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

var dishFormatList = []string{
	"pasta",
	"soup/stew",
	"grain bowl",
	"noodle bowl",
	"salad",
	"wrap/taco",
	"sandwich/burger",
	"stir-fry",
	"curry",
	"flatbread/pizza",
	"casserole/bake",
	"sheet-pan/roast",
	"stuffed vegetable",
	"main with sides",
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

var menuPlanSystemMessage = `
You are a menu planner for independent recipe generators.

Return compact planning labels, not recipes. Use short phrases, generally under 5 words, for cuisine, anchor_ingredient, dish_format, and side_vegetable. Assign exactly one primary category to every recipe: healthy, fancy, local, or different.
Healthy: emphasize ancient or whole grains, more vegetables, lighter proteins and preparations.
Fancy: emphasize richer flavors, more indulgent, higher-calorie ingredients, greater expense, and slower cooking; it may exceed the default one-hour limit.
Local: emphasize produce and proteins associated with the user's region and current season, using available ingredients; do not claim verified local sourcing.
Different: emphasize lesser-known cuisines, regional dishes, and less-familiar seasonings.
Explicit user dietary, time, budget, and equipment constraints take priority over category directions.
Unless the user requests vegetarian, vegan, or another explicit dietary or ingredient direction that overrides this default, use meat or seafood as anchor_ingredient in a strict majority of meals: at least floor(meal count / 2) + 1 (1 of 1, 2 of 3, 3 of 4). Meat or seafood includes poultry, beef, pork, lamb, fish, and shellfish such as shrimp; eggs, dairy, broth, and plant proteins do not count. The meat or seafood must be the anchor, not merely a side, garnish, or mention in recipe_instructions. Healthy meals and requests for more vegetables still follow this default; use lighter meat or seafood and plenty of vegetables. Choose the required meat or seafood anchors before filling any remaining meals with other anchors, while respecting the ingredient availability rules below. For vegetarian or vegan requests, use no meat or seafood and carry the dietary constraint into every meal's recipe_instructions.
Example plan: {"cuisine":"French Bistro","anchor_ingredient":"chicken thighs","dish_format":"grain bowl","side_vegetable":"green beans","category":"healthy","recipe_instructions":["Use the user's anise in this recipe."]}
Choose distinct dish formats across the menu when practical. Use this compact set of canonical formats: ` + strings.Join(dishFormatList, ", ") + `. Choose the dish format before deciding how to cook it. Vegetables may be incorporated into the main dish.
Try and ensure variety across cuisines, anchor ingredients, dish formats, and side vegetables.
Choose anchor_ingredient and side_vegetable from the provided TSV ingredients. Use the exact ingredient Description text from the TSV. Do not choose an unavailable related ingredient; use the available ingredient's name instead. Exception: when the user explicitly asks to use a specific ingredient or says they already have it (including leftovers or a recent purchase), you may choose it as anchor_ingredient or side_vegetable even if it is absent from the TSV. Decide which role best fits the dish unless the user specifies a role. For that exception, anchor_ingredient and side_vegetable must contain only the ingredient name, preserving specific varieties; never put a sentence, ownership statement, or shopping source there. For example, "I have leftover duroc pork" gives anchor_ingredient "duroc pork", and "I got dover sole at Costco" gives anchor_ingredient "dover sole". For "I have extra broccoli", use "broccoli" as either anchor_ingredient or side_vegetable, whichever suits the dish. Include the relevant user direction in recipe_instructions for that recipe. Correct obvious ingredient typos while preserving the user's intent; for example, "I have extra strawberies" refers to "strawberries". A dietary preference, a request for protein, or a direction to avoid an ingredient does not authorize this exception.
Prioritize seasonal ingredients, sale value, practical weeknight cooking.
Assign user directions to recipe_instructions only for the specific recipe plans where they belong. If a user direction applies to every dish, repeat it in every recipe plan's recipe_instructions. If the user mentions having a limited ingredient without asking for it in every dish, assign it to only one fitting recipe.
Return one chef_note_suggestion: concise example feedback the cook could type before asking for a new menu. Tailor it to the planned dishes, available ingredients, seasonality, and likely tradeoffs. It must be 24 characters or fewer, fit in a mobile text box, and be a fragment, not a sentence. Good examples: "less spicy", "faster dinners", "more vegetables", "no seafood".
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
	cacheKey := storeDayPromptCacheKey(location.ID, date.Format("2006-01-02"))

	params := responses.ResponseNewParams{
		Model:        c.model,
		Reasoning:    responses.ReasoningParam{Effort: responses.ReasoningEffortMedium},
		ServiceTier:  c.serviceTier,
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store:              openai.Bool(true),
		Text:               scheme(c.menuSchema),
		PromptCacheKey:     openai.String(cacheKey),
		PromptCacheOptions: defaultCacheOptions(),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)

	return responseToMenuPlan(ctx, aiCategoryMenu, c.model, resp, cacheKey)
}

func (c *client) RegenerateMenuPlan(ctx context.Context, instructions []string, previous ResponseRef, count int) (*MenuPlan, error) {
	if previous.ID == "" {
		return nil, fmt.Errorf("response ID is required for menu plan regeneration")
	}
	if count < 1 {
		return nil, fmt.Errorf("menu plan count must be greater than zero")
	}
	promptMessages := buildRegenerateMenuPlanMessages(instructions, count)

	params := responses.ResponseNewParams{
		Model:              c.model,
		Reasoning:          responses.ReasoningParam{Effort: responses.ReasoningEffortMedium},
		ServiceTier:        c.serviceTier,
		PreviousResponseID: openai.String(previous.ID),
		// Previous response IDs do not carry over top-level instructions.
		// https://developers.openai.com/api/docs/guides/text#message-roles-and-instruction-following
		Instructions: openai.String(menuPlanSystemMessage),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: messagesToInput(promptMessages),
		},
		Store:              openai.Bool(true),
		Text:               scheme(c.menuSchema),
		PromptCacheKey:     openai.String(previous.PromptCacheKey),
		PromptCacheOptions: defaultCacheOptions(),
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate menu plan: %w", err)
	}
	c.recordRecipePrompt(ctx, resp.ID, params, promptMessages)
	return responseToMenuPlan(ctx, aiCategoryMenu, c.model, resp, previous.PromptCacheKey)
}

func responseToMenuPlan(ctx context.Context, category, model string, resp *responses.Response, cacheKey string) (*MenuPlan, error) {
	var plan MenuPlan
	if err := json.Unmarshal([]byte(resp.OutputText()), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse variety plan: %w", err)
	}
	if strings.TrimSpace(resp.ID) == "" {
		return nil, fmt.Errorf("failed to get menu plan response ID")
	}
	plan.ResponseID = resp.ID
	plan.PromptCacheKey = cacheKey
	slog.InfoContext(ctx, "API usage", "ai_category", category, "model", model, "plan", lo.Must(json.Marshal(plan)), responseUsageLogAttr(model, resp.Usage, string(resp.ServiceTier)))
	return &plan, nil
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
	ingredientsPrompt := userPromptMessage(ingredientsMessage)
	ingredientsPrompt.PromptCacheBreakpoint = true
	messages = append(messages, ingredientsPrompt)

	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Build %d distinct recipe plans by default. If the user's directions clearly ask for a different number of recipes, return that many plans instead. Keep the plan count between 1 and 6. Fit the available ingredients, seasonality, and price.", count)),
	)
	cuisines := pickN(cuisineList, 6)
	messages = append(messages, userPromptMessage("For extra variety, loosely draw from one of these cuisine styles if it fits the ingredients: "+strings.Join(cuisines, ", ")))
	// messages = append(messages, userPromptMessage("but don't overlook local cuisine"))

	messages = append(messages, userPromptMessage(categoryDistributionMessage))

	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these previously cooked:\n")
		for _, recipe := range lastRecipes {
			fmt.Fprintf(&prevRecipesMsg, "%s\n", recipe)
		}
		messages = append(messages, userPromptMessage(prevRecipesMsg.String()))
	}

	messages = append(messages, userPromptMessage("Default: cooking methods: oven, stove, grill, slow cooker"))
	messages = append(messages, userPromptMessage("Default: total recipe time, including prep and all timed steps, should stay under 1 hour for non-Fancy meals; Fancy meals may take longer unless the user specifies a time limit"))
	messages = append(messages, userPromptMessage("Default: each recipe should serve 2 people."))
	messages = append(messages, cleanInstructionMessages(instructions)...)
	// Cache both the reusable ingredient prefix and the complete initial menu-plan
	// prompt. Descendant recipe and menu continuations can read these breakpoints;
	// regeneration prompts intentionally do not add new ones.
	messages[len(messages)-1].PromptCacheBreakpoint = true
	return messages, nil
}

func buildRegenerateMenuPlanMessages(instructions []string, count int) []PromptMessage {
	messages := cleanInstructionMessages(instructions)
	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Build %d replacement recipe plan(s) by default. If the user's directions clearly ask for a different number of recipes, return that many plans instead. Keep the plan count between 1 and 6. Avoid passed-on recipe titles and close variants. Fit the user's feedback.", count)),
	)
	messages = append(messages, userPromptMessage(categoryDistributionMessage+" Consider saved meals and their categories from the existing conversation when choosing replacement categories."))

	// Cache the expanded conversation for recipes and later menu regenerations.
	messages[len(messages)-1].PromptCacheBreakpoint = true
	return messages
}
