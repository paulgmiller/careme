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

func TestBuildMenuPlanMessagesIncludesRecipeParentDefaults(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
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

func TestAlignMenuPlanIngredientsAcceptsAvailableIngredientDescriptions(t *testing.T) {
	plan := &MenuPlan{Plans: []RecipePlan{{
		Cuisine:          "Italian",
		AnchorIngredient: "wild caught shrimp",
		Technique:        "pasta",
		SideVegetable:    " broccolini ",
	}}}
	ingredients := []InputIngredient{
		{ProductID: "shrimp-id", Description: "Wild Caught Shrimp"},
		{ProductID: "0000000003277", Description: "Broccolini"},
	}

	err := alignMenuPlanIngredients(plan, ingredients)
	if err != nil {
		t.Fatalf("alignMenuPlanIngredients returned error: %v", err)
	}
	got := plan.Plans[0]
	if got.AnchorIngredient != "wild caught shrimp" {
		t.Fatalf("expected anchor ingredient to be left unchanged, got %q", got.AnchorIngredient)
	}
	if got.SideVegetable != " broccolini " {
		t.Fatalf("expected side vegetable to be left unchanged, got %q", got.SideVegetable)
	}
}

func TestAlignMenuPlanIngredientsRejectsUnavailableIngredientNames(t *testing.T) {
	plan := &MenuPlan{Plans: []RecipePlan{{
		AnchorIngredient: "shrimp",
	}}}
	ingredients := []InputIngredient{{ProductID: "shrimp-id", Description: "Wild Caught Shrimp"}}

	err := alignMenuPlanIngredients(plan, ingredients)

	if err == nil || !strings.Contains(err.Error(), `anchor_ingredient "shrimp" is not an exact ingredient Description from the TSV`) {
		t.Fatalf("expected unavailable ingredient name error, got %v", err)
	}
}

func TestCreateMenuPlanRegeneratesWhenPlanUsesUnavailableIngredient(t *testing.T) {
	recorder := &capturePromptRecorder{}
	var requestBodies []string
	client := NewClient("test-key", "ignored", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBodies = append(requestBodies, string(body))
		responseID := "resp-menu-invalid"
		sideVegetable := "broccoli rabe"
		if len(requestBodies) == 2 {
			responseID = "resp-menu-corrected"
			sideVegetable = "Broccolini"
		}
		return menuPlanHTTPResponse(req, responseID, fmt.Sprintf(`{"plans":[{"cuisine":"Italian","anchor_ingredient":"Wild Caught Shrimp","technique":"pasta","side_vegetable":%q,"fancy":false}]}`, sideVegetable)), nil
	})}, recorder)
	ingredients := []InputIngredient{
		{ProductID: "shrimp-id", Description: "Wild Caught Shrimp"},
		{ProductID: "broccolini-id", Description: "Broccolini"},
	}

	got, err := client.CreateMenuPlan(t.Context(), &locationtypes.Location{State: "WA"}, ingredients, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 1)
	if err != nil {
		t.Fatalf("CreateMenuPlan returned error: %v", err)
	}
	if len(requestBodies) != 2 {
		t.Fatalf("expected initial request and regeneration request, got %d", len(requestBodies))
	}
	if !strings.Contains(requestBodies[1], `"previous_response_id":"resp-menu-invalid"`) {
		t.Fatalf("expected regeneration to continue from invalid response: %s", requestBodies[1])
	}
	if !strings.Contains(requestBodies[1], "broccoli rabe") || !strings.Contains(requestBodies[1], "Description value from the ingredient TSV") {
		t.Fatalf("expected regeneration feedback to describe ingredient mismatch: %s", requestBodies[1])
	}
	if got.ResponseID != "resp-menu-corrected" || got.Plans[0].SideVegetable != "Broccolini" {
		t.Fatalf("unexpected regenerated menu plan: %#v", got)
	}
}

func TestBuildMenuPlanMessagesAddsFancyRequirementForThreePlans(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	date := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)
	messages, err := client.buildMenuPlanMessages(location, nil, nil, date, nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	if !strings.Contains(body, "If doing more than 3 plans mark one plan fancy.") {
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

func TestBuildRegenerateMenuPlanMessagesAddsFancyRequirementForThreePlans(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages(nil, 3)
	body := mustJSON(t, messages)
	if !strings.Contains(body, "make one of the new ones fancy") {
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
	if !strings.Contains(body, "Build 1 replacement recipe plan(s) by default") || !strings.Contains(body, "less spicy") {
		t.Fatalf("unexpected recorded regenerate prompt: %s", body)
	}
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
		return menuPlanHTTPResponse(req, responseID, `{"plans":[{"cuisine":"Korean","anchor_ingredient":"tofu","technique":"stir-fry","side_vegetable":"Broccoli","fancy":false}]}`), nil
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
			}`, responseID, recipePlanModel, outputText))),
		Request: req,
	}
}
