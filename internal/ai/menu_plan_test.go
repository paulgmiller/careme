package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"careme/internal/config"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuildMenuPlanMessagesIncludesRecipeParentDefaults(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	for _, included := range []string{
		"Default: each recipe should serve 2 people.",
		"Default: total recipe time, including prep and all timed steps, should stay under 1 hour",
	} {
		if !strings.Contains(body, included) {
			t.Fatalf("menu planning should include recipe parent default %q: %s", included, body)
		}
	}
}

func TestBuildMenuPlanMessagesUsesRequestedCountAsDefault(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 2)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Build 2 distinct recipe plans by default") {
		t.Fatalf("expected requested default menu plan count in prompt: %s", body)
	}
	if !strings.Contains(body, "If the user's directions clearly ask for a different number of recipes, return that many plans instead") {
		t.Fatalf("expected prompt to let user directions change the count: %s", body)
	}
	if !strings.Contains(body, "Spread meals across healthy, fancy, local, and different categories.") {
		t.Fatalf("expected category distribution requirement: %s", body)
	}
	if strings.Contains(body, "Include one less-common cuisine direction") {
		t.Fatalf("did not expect less-common cuisine requirement for a two-plan request: %s", body)
	}
}

func TestBuildMenuPlanMessagesExcludesIngredientAisleNumbers(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	location := &locationtypes.Location{State: "WA"}
	ingredients := []InputIngredient{{
		ProductID:   "asparagus-1",
		AisleNumber: "SECRET-AISLE-42",
		Description: "Asparagus",
	}}

	messages, err := client.buildMenuPlanMessages(location, ingredients, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 1)
	require.NoError(t, err)

	body := mustJSON(t, messages)
	assert.NotContains(t, body, "AisleNumber")
	assert.NotContains(t, body, "SECRET-AISLE-42")
	assert.Contains(t, body, "ProductId\\tBrand\\tDescription\\tSize\\tPriceRegular\\tPriceSale")
}

func TestBuildMenuPlanMessagesIncludesCuisineListInspiration(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}

	const prefix = "For extra variety, loosely draw from one of these cuisine styles if it fits the ingredients: "
	var inspiration string
	for _, message := range messages {
		if after, ok := strings.CutPrefix(message.Content, prefix); ok {
			inspiration = after
			break
		}
	}
	if inspiration == "" {
		t.Fatalf("expected menu plan prompt to include cuisine inspiration: %s", mustJSON(t, messages))
	}

	included := strings.Split(inspiration, ", ")
	if len(included) == 0 {
		t.Fatalf("expected at least one cuisine from cuisineList in prompt: %q", inspiration)
	}
	for _, cuisine := range included {
		if !slices.Contains(cuisineList, cuisine) {
			t.Fatalf("expected injected cuisine %q to come from cuisineList; prompt cuisines: %v", cuisine, included)
		}
	}
}

func TestRecipePlanInstructionsIncludesPlanSpecificUserDirections(t *testing.T) {
	plan := RecipePlan{
		Cuisine:            "French",
		AnchorIngredient:   "chicken",
		DishFormat:         "braise",
		SideVegetable:      "carrots",
		RecipeInstructions: []string{"use the anise here", "  "},
	}

	got := plan.Instructions()
	if !slices.Contains(got, "User direction for this recipe: use the anise here") {
		t.Fatalf("expected recipe-specific user direction, got %v", got)
	}
	if slices.Contains(got, "User direction for this recipe: ") {
		t.Fatalf("expected blank recipe-specific directions to be skipped, got %v", got)
	}
}

func TestCreateMenuPlanPreservesModelIngredientChoices(t *testing.T) {
	for _, tc := range []struct {
		name        string
		instruction string
		anchor      string
		side        string
		direction   string
	}{
		{name: "user-requested anchor", instruction: "Use halloumi as the main ingredient.", anchor: "halloumi", side: "Broccoli", direction: "Use halloumi as the main ingredient."},
		{name: "leftover anchor", instruction: "I have leftover duroc pork", anchor: "duroc pork", side: "Broccoli", direction: "Use the leftover duroc pork."},
		{name: "purchased anchor", instruction: "I got dover sole at Costco", anchor: "dover sole", side: "Broccoli", direction: "Use the user's dover sole."},
		{name: "user-owned vegetable as anchor", instruction: "I have extra broccoli.", anchor: "broccoli", side: "Carrots", direction: "Use the extra broccoli."},
		{name: "user-owned vegetable as side", instruction: "I have extra broccoli.", anchor: "Chicken", side: "broccoli", direction: "Use the extra broccoli."},
		{name: "corrected anchor spelling", instruction: "I have extra strawberies.", anchor: "strawberries", side: "Spinach", direction: "Use the extra strawberries in a salad."},
		{name: "corrected side spelling", instruction: "I have extra brocoli.", anchor: "Chicken", side: "broccoli", direction: "Use the extra broccoli as a side."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			recorder := &capturePromptRecorder{}
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, 1, requests, "model ingredient choices should not trigger a retry")
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				var params map[string]any
				require.NoError(t, json.Unmarshal(body, &params))
				assert.Contains(t, string(body), tc.instruction)
				assert.Contains(t, string(body), "Correct obvious ingredient typos")
				assert.Contains(t, string(body), "Decide which role best fits the dish")
				assert.Equal(t, config.DefaultRecipeModel, params["model"])
				assert.Contains(t, params["prompt_cache_key"], "careme:store-day:v1:")
				assert.Equal(t, 2, strings.Count(string(body), `"prompt_cache_breakpoint":{"mode":"explicit"}`))
				output, err := json.Marshal(MenuPlan{Plans: []RecipePlan{{AnchorIngredient: tc.anchor, SideVegetable: tc.side, RecipeInstructions: []string{tc.direction}}}})
				require.NoError(t, err)
				return menuPlanHTTPResponse(req, "resp-menu", string(output)), nil
			})}, recorder)
			got, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, []InputIngredient{{Description: "Chicken"}, {Description: "Carrots"}, {Description: "Spinach"}}, []string{tc.instruction}, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 1)
			require.NoError(t, err)
			assert.Equal(t, 1, requests)
			assert.Equal(t, tc.anchor, got.Plans[0].AnchorIngredient)
			assert.Equal(t, tc.side, got.Plans[0].SideVegetable)
			assert.Contains(t, got.Plans[0].Instructions(), "User direction for this recipe: "+tc.direction)
			assert.Equal(t, "resp-menu", got.ResponseID)
			require.NotNil(t, recorder.record)
			assert.Equal(t, got.ResponseID, recorder.record.ResponseID)
		})
	}
}

func TestBuildMenuPlanMessagesAddsCategoryDistribution(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	location := &locationtypes.Location{State: "WA"}
	date := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)
	messages, err := client.buildMenuPlanMessages(location, nil, nil, date, nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Spread meals across healthy, fancy, local, and different categories.") {
		t.Fatalf("expected menu plan prompt to contain category distribution requirement: %s", body)
	}
}

func TestCreateMenuPlanRejectsNonPositiveCount(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	_, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, nil, nil, time.Now(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "menu plan count must be greater than zero") {
		t.Fatalf("expected count error, got %v", err)
	}
}

func TestCreateMenuPlanRecordsPrompt(t *testing.T) {
	const model = "candidate-model"
	recorder := &capturePromptRecorder{}
	client := NewClient(testAIConfig(model), menuPlanResponseClient(t, "resp-menu-create"), recorder)
	ingredients := []InputIngredient{
		{Description: "tofu"},
		{Description: "Broccoli"},
	}

	_, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, ingredients, []string{"make it vegetarian"}, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 2)
	if err != nil {
		t.Fatalf("CreateMenuPlan returned error: %v", err)
	}
	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
	if recorder.record.ResponseID != "resp-menu-create" {
		t.Fatalf("unexpected response id: %#v", recorder.record)
	}
	if recorder.record.Model != model {
		t.Fatalf("unexpected model: %#v", recorder.record)
	}
	if recorder.record.Instructions != strings.TrimSpace(menuPlanSystemMessage) {
		t.Fatalf("unexpected instructions: %q", recorder.record.Instructions)
	}
	body := mustJSON(t, recorder.record.Input)
	if !strings.Contains(body, "Build 2 distinct recipe plans by default") || !strings.Contains(body, "make it vegetarian") {
		t.Fatalf("unexpected recorded menu prompt: %s", body)
	}
	var breakpoints int
	for _, message := range recorder.record.Input {
		if message.PromptCacheBreakpoint {
			breakpoints++
		}
	}
	if breakpoints != 2 || !recorder.record.Input[1].PromptCacheBreakpoint || !recorder.record.Input[len(recorder.record.Input)-1].PromptCacheBreakpoint {
		t.Fatalf("expected breakpoints after ingredients and the complete initial prompt: %#v", recorder.record.Input)
	}
}

func TestBuildRegenerateMenuPlanMessagesUsesReplacementPrompt(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages([]string{"make it vegetarian", "Passed on roast chicken"}, 1)
	for i, message := range messages {
		assert.Equal(t, i == len(messages)-1, message.PromptCacheBreakpoint)
	}
	body := mustJSON(t, messages)
	for _, want := range []string{
		"Build 1 replacement recipe plan(s) by default",
		"If the user's directions clearly ask for a different number of recipes, return that many plans instead",
		"Keep the plan count between 1 and 6",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected replacement prompt to contain %q: %s", want, body)
		}
	}
	if !strings.Contains(body, "make it vegetarian") || !strings.Contains(body, "Passed on roast chicken") {
		t.Fatalf("expected feedback instructions in prompt: %s", body)
	}
}

func TestBuildRegenerateMenuPlanMessagesAddsCategoryDistribution(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages(nil, 3)
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Spread meals across healthy, fancy, local, and different categories.") {
		t.Fatalf("expected regenerate menu plan prompt to contain category distribution requirement: %s", body)
	}
}

func TestRecipePlanInstructions(t *testing.T) {
	plan := RecipePlan{
		Cuisine:          "Korean",
		AnchorIngredient: "tofu",
		DishFormat:       "stir-fry",
		SideVegetable:    "broccoli",
		Category:         MealCategoryFancy,
	}
	got := plan.Instructions()
	if len(got) != 5 {
		t.Fatalf("expected five plan instructions, got %v", got)
	}
	for _, phrase := range []string{
		"Cuisine direction for this recipe: Korean.",
		"Anchor ingredient direction for this recipe: tofu.",
		"Suggested dish format for this recipe: stir-fry.",
		"Side vegetable direction for this recipe: broccoli.",
		"fancier",
	} {
		if !strings.Contains(strings.Join(got, "\n"), phrase) {
			t.Fatalf("expected plan instructions to contain %q, got %v", phrase, got)
		}
	}
}

func TestRegenerateMenuPlanRejectsNonPositiveCount(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	_, err := client.RegenerateMenuPlan(t.Context(), nil, ResponseRef{ID: "resp-menu"}, 0)
	if err == nil || !strings.Contains(err.Error(), "menu plan count must be greater than zero") {
		t.Fatalf("expected count error, got %v", err)
	}
}

func TestRegenerateMenuPlanRecordsPrompt(t *testing.T) {
	recorder := &capturePromptRecorder{}
	client := NewClient(testAIConfig(config.DefaultRecipeModel), menuPlanResponseClient(t, "resp-menu-after"), recorder)

	_, err := client.RegenerateMenuPlan(t.Context(), []string{"less spicy"}, ResponseRef{ID: "resp-menu-before"}, 1)
	if err != nil {
		t.Fatalf("RegenerateMenuPlan returned error: %v", err)
	}
	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
	if recorder.record.ResponseID != "resp-menu-after" || recorder.record.PreviousResponseID != "resp-menu-before" {
		t.Fatalf("unexpected prompt record: %#v", recorder.record)
	}
	body := mustJSON(t, recorder.record.Input)
	if !strings.Contains(body, "Build 1 replacement recipe plan(s) by default") || !strings.Contains(body, "less spicy") {
		t.Fatalf("unexpected recorded regenerate prompt: %s", body)
	}
	for i, message := range recorder.record.Input {
		assert.Equal(t, i == len(recorder.record.Input)-1, message.PromptCacheBreakpoint)
	}
	input := mustJSON(t, messagesToInput(recorder.record.Input))
	assert.Equal(t, 1, strings.Count(input, `"prompt_cache_breakpoint":{"mode":"explicit"}`))
}

func TestMenuPlanSystemMessageIsSpecific(t *testing.T) {
	for _, phrase := range []string{
		"Return compact planning labels, not recipes",
		"short phrases, generally under 5 words",
		"Use the exact ingredient Description text from the TSV",
		"Do not choose an unavailable related ingredient",
		"Do not write recipe steps",
		"chef_note_suggestion",
		"Tailor it to the planned dishes",
		"available ingredients, seasonality",
		"24 characters or fewer",
		"fit in a mobile text box",
		`Good examples: "less spicy", "faster dinners", "more vegetables", "no seafood"`,
		"rationale, or prose notes",
	} {
		if !strings.Contains(menuPlanSystemMessage, phrase) {
			t.Fatalf("expected menu planner system prompt to contain %q", phrase)
		}
	}
}

func TestMenuPlanSchemaExcludesResponseID(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	body := mustJSON(t, client.menuSchema)
	if strings.Contains(body, "response_id") {
		t.Fatalf("menu plan schema should not expose response_id to the model: %s", body)
	}
}

func menuPlanResponseClient(t *testing.T, responseID string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/responses") {
			t.Fatalf("unexpected OpenAI request path: %s", req.URL.Path)
		}
		return menuPlanHTTPResponse(req, responseID, `{"plans":[{"cuisine":"Korean","anchor_ingredient":"tofu","dish_format":"stir-fry","side_vegetable":"Broccoli","category":"healthy"}]}`), nil
	})}
}

func menuPlanHTTPResponse(req *http.Request, responseID, outputText string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id": %q,
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [{
					"id": "msg-menu",
					"type": "message",
					"status": "completed",
					"role": "assistant",
					"content": [{
						"type": "output_text",
						"text": %q,
						"annotations": []
					}]
				}],
				"usage": {
					"input_tokens": 1,
					"input_tokens_details": {"cached_tokens": 0},
					"output_tokens": 1,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 2
				}
			}`, responseID, config.DefaultRecipeModel, outputText))),
		Request: req,
	}
}

func TestRecipePlanDishFormatJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "current field", input: `{"dish_format":"grain bowl"}`, want: "grain bowl"},
		{name: "legacy field ignored", input: `{"technique":"grill"}`, want: ""},
		{name: "current field with legacy field", input: `{"dish_format":"salad","technique":"grill"}`, want: "salad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plan RecipePlan
			require.NoError(t, json.Unmarshal([]byte(tc.input), &plan))
			assert.Equal(t, tc.want, plan.DishFormat)
		})
	}
}

func TestMealCategorySchemaAndSerialization(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	props := client.menuSchema["properties"].(map[string]any)["plans"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	assert.NotContains(t, props, "fancy")
	assert.Equal(t, []any{"healthy", "fancy", "local", "different"}, props["category"].(map[string]any)["enum"])
	for _, category := range []MealCategory{MealCategoryHealthy, MealCategoryFancy, MealCategoryLocal, MealCategoryDifferent} {
		t.Run(string(category), func(t *testing.T) {
			plan := RecipePlan{Category: category}
			body, err := json.Marshal(plan)
			require.NoError(t, err)
			assert.Contains(t, string(body), `"category":"`+string(category)+`"`)
			assert.NotContains(t, string(body), `"fancy":`)
			var decoded RecipePlan
			require.NoError(t, json.Unmarshal(body, &decoded))
			assert.Equal(t, plan, decoded)
			body, err = yaml.Marshal(plan)
			require.NoError(t, err)
			assert.Contains(t, string(body), "category: "+string(category))
			require.NoError(t, yaml.Unmarshal(body, &decoded))
			assert.Equal(t, plan, decoded)
		})
	}
}

func TestRecipePlanCategoryInstructions(t *testing.T) {
	for _, tc := range []struct {
		category MealCategory
		phrases  []string
	}{
		{MealCategoryHealthy, []string{"ancient or whole grains", "more vegetables", "lighter proteins"}},
		{MealCategoryFancy, []string{"richer flavors", "greater expense", "slower cooking", "may exceed the default one-hour limit"}},
		{MealCategoryLocal, []string{"user's region", "current season", "available ingredients", "do not claim verified local sourcing"}},
		{MealCategoryDifferent, []string{"lesser-known cuisines", "regional dishes", "less-familiar seasonings"}},
	} {
		t.Run(string(tc.category), func(t *testing.T) {
			plan := RecipePlan{Category: tc.category, RecipeInstructions: []string{"Keep it under 30 minutes."}}
			instructions := strings.Join(plan.Instructions(), "\n")
			for _, phrase := range tc.phrases {
				assert.Contains(t, instructions, phrase)
			}
			assert.Contains(t, instructions, "Explicit user dietary, time, budget, and equipment constraints still apply")
			assert.Contains(t, instructions, "User direction for this recipe: Keep it under 30 minutes.")
		})
	}
}

func TestLegacyRecipePlansRemainReadable(t *testing.T) {
	for _, fancy := range []bool{false, true} {
		var plan RecipePlan
		require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"cuisine":"French","fancy":%t}`, fancy)), &plan))
		assert.Empty(t, plan.Category)
		assert.Equal(t, "French", plan.Cuisine)
		assert.Len(t, plan.Instructions(), 4)
	}
}

func TestMenuCategoryDistributionForCounts(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, nil)
	for _, count := range []int{1, 3, 4, 6} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			initial, err := client.buildMenuPlanMessages(&locationtypes.Location{State: "WA"}, nil, []string{"Make every meal healthy."}, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, count)
			require.NoError(t, err)
			replacement := buildRegenerateMenuPlanMessages([]string{"Saved: a local salmon dinner", "Make every meal healthy."}, count)
			for _, messages := range [][]PromptMessage{initial, replacement} {
				body := mustJSON(t, messages)
				assert.Contains(t, body, "For up to four meals, choose distinct categories")
				assert.Contains(t, body, "cover all four before repeating")
				assert.Contains(t, body, "Explicit user requests take priority over category variety")
				assert.Contains(t, body, "Make every meal healthy.")
				assert.NotContains(t, body, "make sure one of the saved meals or those in the meal plan is fancy")
			}
			body := mustJSON(t, replacement)
			assert.Contains(t, body, "Consider saved meals and their categories from the existing conversation")
			assert.Contains(t, body, "Saved: a local salmon dinner")
			assert.Contains(t, mustJSON(t, initial), "Fancy meals may take longer unless the user specifies a time limit")
		})
	}
}
