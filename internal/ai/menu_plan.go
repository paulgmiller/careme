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

const recipePlanModel = openai.ChatModelGPT5Mini // need to play witht this

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

Return compact planning labels, not recipes. Use short phrases, generally under 5 words, for cuisine, anchor_ingredient, and technique. Set fancy to true only for the richer/splurgier/time intensive option.
Example plan: {"cuisine":"French Bistro","anchor_ingredient":"chicken thighs","technique":"braise","side_vegetable":"green beans","fancy":false}
Try and ensure variety across cuisines, anchor ingredients, techniques, and side vegetables.
Prioritize seasonal ingredients, sale value, practical weeknight cooking. 
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

	return responseToMenuPlan(ctx, resp)
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
) ([]PromptMessage, error) {
	messages, err := c.buildRecipeContextMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, err
	}
	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Build exactly %d distinct recipe plans that fit the available ingredients, seasonality, and price.", count)),
	)
	cuisines := pickN(cuisineList, 5)
	messages = append(messages, userPromptMessage("For extra variety, loosely draw from one of these cuisine styles if it fits the ingredients: "+strings.Join(cuisines, ", ")))
	// messages = append(messages, userPromptMessage("but don't overlook local cuisine"))
	if count >= 3 {
		messages = append(messages, userPromptMessage("Mark one plan fancy."))
		// messages = append(messages, userPromptMessage("Include one less-common cuisine direction."))
	}
	return messages, nil
}

func buildRegenerateMenuPlanMessages(instructions []string, count int) []PromptMessage {
	messages := cleanInstructionMessages(instructions)
	messages = append(messages,
		userPromptMessage(fmt.Sprintf("Pick exactly %d replacement plans. Avoid passed-on recipe titles and close variants. Fit the user's feedback.", count)),
	)
	// ideally do this if they dismissed fancy.
	if count >= 3 {
		messages = append(messages, userPromptMessage("Mark one replacement plan fancy."))
		// messages = append(messages, userPromptMessage("Include one less-common cuisine direction."))
	}
	return messages
}
