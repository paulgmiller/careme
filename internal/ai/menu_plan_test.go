package ai

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	locationtypes "careme/internal/locations/types"
)

func TestMenuPlanAndRecipeMessagesShareCachePrefix(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	ingredients := []InputIngredient{
		{ProductID: "chicken-1", Description: "Chicken thighs", Size: "2 lb", PriceRegular: new(float32)},
		{ProductID: "beans-1", Description: "Green beans", Size: "12 oz", PriceRegular: new(float32)},
	}
	*ingredients[0].PriceRegular = 8.99
	*ingredients[1].PriceRegular = 2.99
	instructions := []string{"make it high protein"}
	lastRecipes := []string{"Lemon chicken pasta"}
	date := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)

	contextMessages, err := client.buildSharedContextMessages(location, ingredients, date, lastRecipes)
	if err != nil {
		t.Fatalf("buildSharedContextMessages returned error: %v", err)
	}
	menuMessages, err := client.buildMenuPlanMessages(location, ingredients, instructions, date, lastRecipes, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}

	prefixLen := len(contextMessages)
	if prefixLen == 0 {
		t.Fatal("expected shared context prefix")
	}
	if got, want := mustJSON(t, menuMessages[:prefixLen]), mustJSON(t, contextMessages); got != want {
		t.Fatalf("menu planning should share prompt prefix with recipe context:\ngot  %s\nwant %s", got, want)
	}
	for _, message := range contextMessages {
		if message.Role != "user" {
			t.Fatalf("expected only user prompt messages, got %#v", contextMessages)
		}
	}
}

func TestBuildMenuPlanMessagesOmitsRecipeOnlyDefaults(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	for _, excluded := range []string{
		"Default: each recipe should serve 2 people.",
		"Default: total recipe time, including prep and all timed steps, should stay under 1 hour",
	} {
		if strings.Contains(body, excluded) {
			t.Fatalf("menu planning should not receive recipe-only default %q: %s", excluded, body)
		}
	}
}

func TestBuildMenuPlanMessagesUsesRequestedCountAsDefault(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
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
	if strings.Contains(body, "Mark one plan fancy") {
		t.Fatalf("did not expect fancy-plan requirement for a two-plan request: %s", body)
	}
	if strings.Contains(body, "Include one less-common cuisine direction") {
		t.Fatalf("did not expect less-common cuisine requirement for a two-plan request: %s", body)
	}
}

func TestBuildMenuPlanMessagesIncludesCuisineListInspiration(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}

	const prefix = "For extra variety, loosely draw from one of these cuisine styles if it fits the ingredients: "
	var inspiration string
	for _, message := range messages {
		if strings.HasPrefix(message.Content, prefix) {
			inspiration = strings.TrimPrefix(message.Content, prefix)
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
		Technique:          "braise",
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

func TestBuildMenuPlanMessagesAddsFancyRequirementForThreePlans(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Mark one plan fancy") {
		t.Fatalf("expected menu plan prompt to contain fancy requirement: %s", body)
	}
}

func TestCreateMenuPlanRejectsNonPositiveCount(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	_, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, nil, nil, time.Now(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "menu plan count must be greater than zero") {
		t.Fatalf("expected count error, got %v", err)
	}
}

func TestCreateMenuPlanRecordsPrompt(t *testing.T) {
	recorder := &capturePromptRecorder{}
	client := NewClient("test-key", "ignored", menuPlanResponseClient(t, "resp-menu-create"), recorder)

	_, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, nil, []string{"make it vegetarian"}, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 2)
	if err != nil {
		t.Fatalf("CreateMenuPlan returned error: %v", err)
	}
	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
	if recorder.record.ResponseID != "resp-menu-create" {
		t.Fatalf("unexpected response id: %#v", recorder.record)
	}
	if recorder.record.Model != string(recipePlanModel) {
		t.Fatalf("unexpected model: %#v", recorder.record)
	}
	if recorder.record.Instructions != strings.TrimSpace(menuPlanSystemMessage) {
		t.Fatalf("unexpected instructions: %q", recorder.record.Instructions)
	}
	body := mustJSON(t, recorder.record.Input)
	if !strings.Contains(body, "Build 2 distinct recipe plans by default") || !strings.Contains(body, "make it vegetarian") {
		t.Fatalf("unexpected recorded menu prompt: %s", body)
	}
}

func TestBuildRegenerateMenuPlanMessagesUsesReplacementPrompt(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages([]string{"make it vegetarian", "Passed on roast chicken"}, 1)
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Pick exactly 1 replacement plans") {
		t.Fatalf("expected replacement count in prompt: %s", body)
	}
	if !strings.Contains(body, "make it vegetarian") || !strings.Contains(body, "Passed on roast chicken") {
		t.Fatalf("expected feedback instructions in prompt: %s", body)
	}
}

func TestBuildRegenerateMenuPlanMessagesAddsFancyRequirementForThreePlans(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages(nil, 3)
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Mark one replacement plan fancy") {
		t.Fatalf("expected regenerate menu plan prompt to contain fancy requirement: %s", body)
	}
}

func TestRecipePlanInstructions(t *testing.T) {
	plan := RecipePlan{
		Cuisine:          "Korean",
		AnchorIngredient: "tofu",
		Technique:        "stir-fry",
		SideVegetable:    "broccoli",
		Fancy:            true,
	}
	got := plan.Instructions()
	if len(got) != 5 {
		t.Fatalf("expected five plan instructions, got %v", got)
	}
	for _, phrase := range []string{
		"Cuisine direction for this recipe: Korean.",
		"Anchor ingredient direction for this recipe: tofu.",
		"Suggested technique for this recipe: stir-fry.",
		"Side vegetable direction for this recipe: broccoli.",
		"fancier",
	} {
		if !strings.Contains(strings.Join(got, "\n"), phrase) {
			t.Fatalf("expected plan instructions to contain %q, got %v", phrase, got)
		}
	}
}

func TestRegenerateMenuPlanRejectsNonPositiveCount(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	_, err := client.RegenerateMenuPlan(t.Context(), nil, "resp-menu", 0)
	if err == nil || !strings.Contains(err.Error(), "menu plan count must be greater than zero") {
		t.Fatalf("expected count error, got %v", err)
	}
}

func TestRegenerateMenuPlanRecordsPrompt(t *testing.T) {
	recorder := &capturePromptRecorder{}
	client := NewClient("test-key", "ignored", menuPlanResponseClient(t, "resp-menu-after"), recorder)

	_, err := client.RegenerateMenuPlan(t.Context(), []string{"less spicy"}, "resp-menu-before", 1)
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
	if !strings.Contains(body, "Pick exactly 1 replacement plans") || !strings.Contains(body, "less spicy") {
		t.Fatalf("unexpected recorded regenerate prompt: %s", body)
	}
}

func TestMenuPlanSystemMessageIsSpecific(t *testing.T) {
	for _, phrase := range []string{
		"Return compact planning labels, not recipes",
		"short phrases, generally under 5 words",
		"Do not write recipe steps",
		"rationale, or prose notes",
	} {
		if !strings.Contains(menuPlanSystemMessage, phrase) {
			t.Fatalf("expected menu planner system prompt to contain %q", phrase)
		}
	}
}

func TestMenuPlanSchemaExcludesResponseID(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
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
						"text": "{\"plans\":[{\"cuisine\":\"Korean\",\"anchor_ingredient\":\"tofu\",\"technique\":\"stir-fry\",\"fancy\":false}]}",
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
			}`, responseID, recipePlanModel))),
			Request: req,
		}, nil
	})}
}
