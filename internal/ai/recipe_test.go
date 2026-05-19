package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	locationtypes "careme/internal/locations/types"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestRecipeComputeHash(t *testing.T) {
	recipe := Recipe{
		Title:        "Test Recipe",
		Description:  "A delicious test recipe",
		CookTime:     "35 minutes",
		CostEstimate: "$18-24",
		Ingredients: []Ingredient{
			{Name: "Ingredient 1", Quantity: "1 cup", Price: "2.99"},
			{Name: "Ingredient 2", Quantity: "2 tbsp", Price: "0.99"},
		},
		Instructions: []string{"Step 1", "Step 2"},
		Health:       "Healthy",
		DrinkPairing: "Red Wine",
	}

	hash1 := recipe.ComputeHash()
	if hash1 == "" {
		t.Fatal("hash should not be empty")
	}
	if hash1 != "YK2TFI6O3tGLPAxPjqMPEw==" {
		t.Fatalf("Hash changed by json marshalling: %s", hash1)
	}

	recipe.OriginHash = "somehashvalue"
	recipe.ParentHash = "parenthashvalue"

	// Hash should be consistent regardless of provenance fields.
	hash2 := recipe.ComputeHash()
	if hash1 != hash2 {
		t.Fatalf("hash should be consistent: %s != %s", hash1, hash2)
	}

	// Different recipe should have different hash
	recipe2 := recipe
	recipe2.Title = "Different Recipe"
	hash3 := recipe2.ComputeHash()
	if hash1 == hash3 {
		t.Fatalf("different recipes should have different hashes")
	}
}

func TestRecipeHashLength(t *testing.T) {
	recipe := Recipe{
		Title: "Simple Recipe",
	}

	hash := recipe.ComputeHash()
	// fnv 128 url encoded is 24
	if len(hash) != 24 {
		t.Fatalf("expected hash length of 24, got %d", len(hash))
	}
}

func TestNewClientUsesGPT55ForRecipeFlow(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, &capturePromptRecorder{})

	if client.model != "gpt-5.5" {
		t.Fatalf("expected primary recipe model to be gpt-5.5, got %q", client.model)
	}
	if client.wineModel != openai.ChatModelGPT5Mini {
		t.Fatalf("expected wine model to remain low-cost mini path, got %q", client.wineModel)
	}
}

func TestRecipeSchemaLeavesServerOwnedIngredientFieldsOut(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	properties := schemaProperties(t, client.recipeSchema)
	ingredients := schemaObject(t, properties["ingredients"])
	items := schemaObject(t, ingredients["items"])
	ingredientProperties := schemaProperties(t, items)
	ingredientRequired := schemaRequired(t, items)

	if _, ok := ingredientProperties["id"]; !ok {
		t.Fatalf("expected ingredient schema to include product id")
	}
	if !slices.Contains(ingredientRequired, "id") {
		t.Fatalf("expected ingredient schema to require product id, got %v", ingredientRequired)
	}
	if _, ok := ingredientProperties["name"]; !ok {
		t.Fatalf("expected ingredient schema to include name")
	}
	if _, ok := ingredientProperties["quantity"]; !ok {
		t.Fatalf("expected ingredient schema to include quantity")
	}
	if _, ok := ingredientProperties["price"]; ok {
		t.Fatalf("did not expect model schema to include server-owned price")
	}
	if _, ok := ingredientProperties["aisle_number"]; ok {
		t.Fatalf("did not expect model schema to include server-owned aisle number")
	}
}

func schemaRequired(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", schema["required"])
	}
	required := make([]string, 0, len(raw))
	for _, value := range raw {
		field, ok := value.(string)
		if !ok {
			t.Fatalf("expected required field string, got %#v", value)
		}
		required = append(required, field)
	}
	return required
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", schema["properties"])
	}
	return properties
}

func schemaObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected schema object, got %#v", value)
	}
	return object
}

func TestNormalizeWineStyle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain style", in: "Pinot Noir", want: "Pinot Noir"},
		{name: "parenthetical region hint", in: "Sauvignon Blanc (New Zealand or Loire)", want: "Sauvignon Blanc"},
		{name: "trailing punctuation", in: "  Riesling.  ", want: "Riesling"},
		{name: "bracket hint", in: "Chardonnay [California]", want: "Chardonnay"},
		{name: "empty", in: "   ", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeWineStyle(tc.in); got != tc.want {
				t.Fatalf("normalizeWineStyle(%q): got %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeRecipeWineStyles(t *testing.T) {
	got := normalizeRecipeWineStyles([]string{
		" Pinot Noir (WA or Oregon) ",
		"pinot noir",
		"Sauvignon Blanc (New Zealand or Loire)",
		"Riesling",
	})
	want := []string{"Pinot Noir", "Sauvignon Blanc"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected normalized wine styles: got %#v want %#v", got, want)
	}
}

func TestBuildRecipeImagePrompt(t *testing.T) {
	recipe := Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []Ingredient{{Name: "Chicken", Quantity: "1 whole"}},
		Instructions: []string{"Roast until golden."},
	}

	prompt, err := buildRecipeImagePrompt(recipe)
	if err != nil {
		t.Fatalf("buildRecipeImagePrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "realistic overhead food photograph") {
		t.Fatalf("expected image prompt instructions in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Recipe:\nRoast Chicken\nCrisp skin and herbs.\nInstructions:\n- Roast until golden.\n") {
		t.Fatalf("expected recipe summary in prompt: %s", prompt)
	}
}

func TestBuildWineSelectionPrompt(t *testing.T) {
	recipe := Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		CookTime:     "45 minutes",
		CostEstimate: "$18-24",
		Ingredients: []Ingredient{
			{Name: "Chicken", Quantity: "1 whole", Price: "$12"},
			{Name: "Lemon", Quantity: "1", Price: "$1"},
		},
		Instructions: []string{"Roast until golden.", "Finish with lemon juice."},
		Health:       "Balanced dinner",
		DrinkPairing: "Pinot Noir",
		WineStyles:   []string{"Pinot Noir", "Chardonnay"},
	}
	wines := []InputIngredient{
		{ProductID: "pinot-noir-1", Description: "Pinot Noir", Size: "750mL", PriceRegular: new(float32(13.99))},
	}

	prompt, err := buildWineSelectionPrompt(recipe, wines)
	if err != nil {
		t.Fatalf("buildWineSelectionPrompt returned error: %v", err)
	}
	expect := "Chicken\nCrisp skin and herbs."
	if !strings.Contains(prompt, expect) {
		t.Fatalf("expected recipe summary in prompt: %s\n\n got \n %s", expect, prompt)
	}
	if !strings.Contains(prompt, "Existing drink pairing note: Pinot Noir") {
		t.Fatalf("expected pairing hints in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "- Roast until golden.\n- Finish with lemon juice.\n") {
		t.Fatalf("expected instructions replay in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Candidate wines TSV:\nProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\npinot-noir-1\t\t\tPinot Noir\t750mL\t13.99\t13.99\n") {
		t.Fatalf("expected candidate wines TSV in prompt: %s", prompt)
	}
}

func TestMenuPlanAndRecipeMessagesShareCachePrefix(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	ingredients := []InputIngredient{
		{ProductID: "chicken-1", Description: "Chicken thighs", Size: "2 lb", PriceRegular: new(float32(8.99))},
		{ProductID: "beans-1", Description: "Green beans", Size: "12 oz", PriceRegular: new(float32(2.99))},
	}
	instructions := []string{"make it high protein"}
	lastRecipes := []string{"Lemon chicken pasta"}
	date := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)

	contextMessages, err := client.buildRecipeContextMessages(location, ingredients, instructions, date, lastRecipes)
	if err != nil {
		t.Fatalf("buildRecipeContextMessages returned error: %v", err)
	}
	menuMessages, err := client.buildMenuPlanMessages(location, ingredients, instructions, date, lastRecipes, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	recipeInstructions := append(slices.Clone(instructions), RecipePlan{
		Cuisine:          "Korean",
		AnchorIngredient: "chicken thighs",
		Technique:        "stir-fry",
	}.Instructions()...)
	recipeMessages, err := client.buildRecipeMessages(location, ingredients, recipeInstructions, date, lastRecipes)
	if err != nil {
		t.Fatalf("buildRecipeMessages returned error: %v", err)
	}

	prefixLen := len(contextMessages)
	if prefixLen == 0 {
		t.Fatal("expected shared context prefix")
	}
	if got, want := mustJSON(t, menuMessages[:prefixLen]), mustJSON(t, recipeMessages[:prefixLen]); got != want {
		t.Fatalf("menu planning and recipe generation should share prompt prefix:\ngot  %s\nwant %s", got, want)
	}
	if got, want := mustJSON(t, contextMessages), mustJSON(t, recipeMessages[:prefixLen]); got != want {
		t.Fatalf("recipe generation prefix should match shared context:\ngot  %s\nwant %s", got, want)
	}
	for _, message := range recipeMessages {
		if message.Role != "user" {
			t.Fatalf("expected only user prompt messages, got %#v", recipeMessages)
		}
	}
}

func TestBuildMenuPlanMessagesUsesRequestedCount(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 2)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	if !strings.Contains(body, "Build exactly 2 distinct recipe plans") {
		t.Fatalf("expected requested menu plan count in prompt: %s", body)
	}
	if strings.Contains(body, "Mark one plan fancy") {
		t.Fatalf("did not expect fancy-plan requirement for a two-plan request: %s", body)
	}
	if strings.Contains(body, "Include one less-common cuisine direction") {
		t.Fatalf("did not expect less-common cuisine requirement for a two-plan request: %s", body)
	}
}

func TestBuildMenuPlanMessagesAddsVarietyRequirementsForThreePlans(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	location := &locationtypes.Location{State: "WA"}
	messages, err := client.buildMenuPlanMessages(location, nil, nil, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), nil, 3)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	body := mustJSON(t, messages)
	for _, want := range []string{
		"Mark one plan fancy",
		"Include one less-common cuisine direction",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected menu plan prompt to contain %q: %s", want, body)
		}
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
	if !strings.Contains(body, "Build exactly 2 distinct recipe plans") || !strings.Contains(body, "make it vegetarian") {
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

func TestBuildRegenerateMenuPlanMessagesAddsVarietyRequirementsForThreePlans(t *testing.T) {
	messages := buildRegenerateMenuPlanMessages(nil, 3)
	body := mustJSON(t, messages)
	for _, want := range []string{
		"Mark one replacement plan fancy",
		"Include one less-common cuisine direction",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected regenerate menu plan prompt to contain %q: %s", want, body)
		}
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(body)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSystemMessageRequiresPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"start with prep such as preheating, chopping, slicing, dicing, mixing, or make-ahead work before active cooking",
		"do not rely on prep details from the ingredient list alone",
		"provide the total elapsed recipe time",
		"5 to 8 clear steps",
		"Ensure cook_time reflects the total time implied by every instruction step, including prep, resting, and passive cooking time.",
		"set id to the exact ProductId",
		"amount used in the recipe as quantity",
	} {
		if !strings.Contains(systemMessage, want) {
			t.Fatalf("expected system message to contain %q", want)
		}
	}
}

func TestResponseUsageLogAttr(t *testing.T) {
	attr := responseUsageLogAttr(responses.ResponseUsage{
		InputTokens:  1200,
		OutputTokens: 350,
		TotalTokens:  1550,
		InputTokensDetails: responses.ResponseUsageInputTokensDetails{
			CachedTokens: 900,
		},
		OutputTokensDetails: responses.ResponseUsageOutputTokensDetails{
			ReasoningTokens: 125,
		},
	})

	if attr.Key != "usage" {
		t.Fatalf("unexpected attr key: %s", attr.Key)
	}
	if attr.Value.Kind() != slog.KindGroup {
		t.Fatalf("unexpected attr kind: %v", attr.Value.Kind())
	}
	if !reflect.DeepEqual(attr.Value.Group(), []slog.Attr{
		slog.Int64("inputTokens", 1200),
		slog.Group("inputTokensDetails", slog.Int64("cachedTokens", 900)),
		slog.Int64("outputTokens", 350),
		slog.Group("outputTokensDetails", slog.Int64("reasoningTokens", 125)),
		slog.Int64("totalTokens", 1550),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}

func TestImageUsageLogAttr(t *testing.T) {
	attr := imageUsageLogAttr(openai.ImagesResponseUsage{
		InputTokens:  100,
		OutputTokens: 200,
		TotalTokens:  300,
		InputTokensDetails: openai.ImagesResponseUsageInputTokensDetails{
			ImageTokens: 60,
			TextTokens:  40,
		},
		OutputTokensDetails: openai.ImagesResponseUsageOutputTokensDetails{
			ImageTokens: 180,
			TextTokens:  20,
		},
	})

	if attr.Key != "usage" {
		t.Fatalf("unexpected attr key: %s", attr.Key)
	}
	if attr.Value.Kind() != slog.KindGroup {
		t.Fatalf("unexpected attr kind: %v", attr.Value.Kind())
	}
	if !reflect.DeepEqual(attr.Value.Group(), []slog.Attr{
		slog.Int64("inputTokens", 100),
		slog.Group("inputTokensDetails",
			slog.Int64("imageTokens", 60),
			slog.Int64("textTokens", 40),
		),
		slog.Int64("outputTokens", 200),
		slog.Group("outputTokensDetails",
			slog.Int64("imageTokens", 180),
			slog.Int64("textTokens", 20),
		),
		slog.Int64("totalTokens", 300),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}
