package ai

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRecipeComputeHashIncludesStructuredPropertiesWithoutChangingLegacyHash(t *testing.T) {
	legacy := Recipe{
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
	require.Equal(t, "YK2TFI6O3tGLPAxPjqMPEw==", legacy.ComputeHash())

	structured := legacy
	structured.Properties = RecipeProperties{
		TotalMinutes:         35,
		Servings:             4,
		EstimatedCostDollars: 21,
		CaloriesPerServing:   540,
		CookingMethods:       []CookingMethod{CookingMethodStovetop, CookingMethodOven},
	}
	assert.NotEqual(t, legacy.ComputeHash(), structured.ComputeHash())
}

func TestRecipeComputeHashSeparatesAdjacentStructuredPropertyValues(t *testing.T) {
	first := Recipe{Properties: RecipeProperties{TotalMinutes: 3, Servings: 54}}
	second := Recipe{Properties: RecipeProperties{TotalMinutes: 35, Servings: 4}}

	assert.NotEqual(t, first.ComputeHash(), second.ComputeHash())
}

func TestRecipeInstructionMarkdownContributesToHash(t *testing.T) {
	base := Recipe{Instructions: []string{"Prepare the sauce:\n\n- 1 tablespoon olive oil"}}
	different := Recipe{Instructions: []string{"Prepare the sauce:\n\n- 2 tablespoons olive oil"}}

	if base.ComputeHash() == different.ComputeHash() {
		t.Fatal("instruction Markdown should contribute to recipe hashes")
	}
}

func TestRecipeDecodesAndPreservesInstructions(t *testing.T) {
	const body = `{"title":"Soup","instructions":["Chop the onion.","Simmer the soup."]}`
	var recipe Recipe
	if err := json.Unmarshal([]byte(body), &recipe); err != nil {
		t.Fatalf("decode recipe: %v", err)
	}
	if !reflect.DeepEqual(recipe.Instructions, []string{"Chop the onion.", "Simmer the soup."}) {
		t.Fatalf("unexpected instructions: %+v", recipe.Instructions)
	}

	encoded, err := json.Marshal(recipe)
	if err != nil {
		t.Fatalf("encode recipe: %v", err)
	}
	if !strings.Contains(string(encoded), `"instructions":["Chop the onion.","Simmer the soup."]`) {
		t.Fatalf("instructions should retain their wire shape: %s", encoded)
	}
}

func TestRecipeSerializesMarkdownInstructions(t *testing.T) {
	recipe := Recipe{Instructions: []string{
		"Prepare the vegetables:\n\n- 1 pepper, diced\n- 2 onions, sliced\n- 3 garlic cloves, minced",
	}}
	encoded, err := json.Marshal(recipe)
	if err != nil {
		t.Fatalf("encode recipe: %v", err)
	}
	want := `"instructions":["Prepare the vegetables:\n\n- 1 pepper, diced\n- 2 onions, sliced\n- 3 garlic cloves, minced"]`
	if !strings.Contains(string(encoded), want) {
		t.Fatalf("recipe JSON should contain %s: %s", want, encoded)
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

func TestRecipeSchemaUsesStructuredProperties(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	properties := schemaProperties(t, client.recipeSchema)

	assert.Contains(t, properties, "properties")
	assert.NotContains(t, properties, "cook_time")
	assert.NotContains(t, properties, "cost_estimate")
	assert.Contains(t, properties, "health")
	assert.Contains(t, schemaRequired(t, client.recipeSchema), "health")

	recipeProperties := schemaObject(t, properties["properties"])
	fields := schemaProperties(t, recipeProperties)
	for _, name := range []string{
		"total_minutes",
		"servings",
		"estimated_cost_dollars",
		"calories_per_serving",
		"cooking_methods",
	} {
		assert.Contains(t, fields, name)
		assert.Contains(t, schemaRequired(t, recipeProperties), name)
	}
	assert.NotContains(t, fields, "health_note")
	for _, name := range []string{"total_minutes", "servings", "estimated_cost_dollars", "calories_per_serving"} {
		field := schemaObject(t, fields[name])
		assert.Equal(t, "integer", field["type"])
		assert.Equal(t, float64(1), field["minimum"])
	}

	methods := schemaObject(t, fields["cooking_methods"])
	assert.Equal(t, float64(1), methods["minItems"])
	assert.NotContains(t, methods, "uniqueItems")
	methodItems := schemaObject(t, methods["items"])
	enum, ok := methodItems["enum"].([]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []any{"stovetop", "oven", "grill", "slow_cooker", "air_fryer", "no_cook", "other"}, enum)
	assert.NotContains(t, enum, "microwave")
}

func TestRecipeSchemaUsesStringInstructions(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	properties := schemaProperties(t, client.recipeSchema)
	instructions := schemaObject(t, properties["instructions"])
	items := schemaObject(t, instructions["items"])
	if items["type"] != "string" {
		t.Fatalf("expected instruction items to be strings, got %#v", items)
	}
}

func TestSystemMessageRequiresPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"start with prep such as preheating, chopping, slicing, dicing, mixing, or make-ahead work before active cooking",
		"do not rely on prep details from the ingredient list alone",
		"provide the total elapsed recipe time",
		"Ensure properties.total_minutes reflects the total time implied by every instruction step, including prep, resting, and passive cooking time.",
		"properties.servings: provide the integer number of people served",
		"properties.estimated_cost_dollars: provide one integer estimate",
		"properties.calories_per_serving: provide a reasonable integer calorie estimate for one serving",
		"choosing only stovetop, oven, grill, slow_cooker, air_fryer, no_cook, or other",
		"Use other only when the primary cooking method is outside the named choices, such as smoking, pressure cooking, or sous vide.",
		"health: use one short sentence only when explaining a deliberate dietary or nutritional ingredient swap",
		"otherwise return an empty string",
		"Do not imply that gluten-free food is inherently healthier.",
		"use as many clear steps as the work requires",
		"Each step should cover one coherent task or component whose actions are naturally done together.",
		"Do not combine unrelated work to limit the number of steps.",
		"Keep immediate actions on the same ingredient in the same step.",
		"place a \"- \" bullet list at the point those ingredients enter the action",
		"Put a blank line before the first bullet and after the final bullet so surrounding prose stays outside the list.",
		"continue with prose after the list when the action continues",
		"Do not use lists for cooking, resting, serving, plating, one primary ingredient",
		"Do not use HTML or other Markdown.",
		"later steps should refer to that component by name without restating its ingredients or their amounts",
		"set id to the exact ProductId",
		"Set quantity to the total amount needed across the entire recipe",
		"Every time a step first uses an ingredient, including a pantry ingredient, state its exact amount in the prose or a bullet.",
		"When an ingredient is divided among steps, the step amounts must add up to the total quantity in ingredients.",
		`Do not use an unquantified phrase such as "the remaining oil"`,
		"Cross-check every ingredient mention in instruction prose and bullets for an exact step-level amount",
		"Presalting meat and salting pasta or blanching water season food during cooking.",
		"Do not reduce or omit those applications merely because salt or salty ingredients are added later; adjust finishing salt instead.",
		"recommend the doneness that best suits the dish and give one concise target or pull temperature",
		"Do not name the FDA, USDA, or other government agencies; quote official food-safety guidance; compare the recommended doneness with alternate regulatory temperatures; or add a temperature disclaimer.",
		"Careme provides a separate temperature guide beside the recipe.",
	} {
		if !strings.Contains(systemMessage, want) {
			t.Fatalf("expected system message to contain %q", want)
		}
	}
}

func TestGenerateRecipeUsesMenuResponseIDWithoutIngredientTSV(t *testing.T) {
	recorder := &capturePromptRecorder{}
	var requestBody string
	client := NewClient("test-key", "ignored", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/responses") {
			t.Fatalf("unexpected OpenAI request path: %s", req.URL.Path)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id": "resp-recipe",
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [{
					"id": "msg-recipe",
					"type": "message",
					"status": "completed",
					"role": "assistant",
					"content": [{
						"type": "output_text",
						"text": "{\"title\":\"Korean Chicken\",\"description\":\"Fast dinner.\",\"properties\":{\"total_minutes\":35,\"servings\":4,\"estimated_cost_dollars\":12,\"calories_per_serving\":510,\"cooking_methods\":[\"stovetop\"]},\"ingredients\":[],\"instructions\":[\"Prep.\"],\"health\":\"\",\"drink_pairing\":\"Water.\",\"wine_styles\":[]}",
						"annotations": []
					}]
				}],
				"usage": {
					"input_tokens": 20,
					"input_tokens_details": {"cached_tokens": 15},
					"output_tokens": 5,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 25
				}
			}`, defaultRecipeModel))),
			Request: req,
		}, nil
	})}, recorder)

	cacheKey := storeDayPromptCacheKey("store-123", time.Date(2026, time.August, 4, 0, 0, 0, 0, time.UTC).Format("2006-01-02"))
	menu := ResponseRef{ID: "resp-menu-plan", PromptCacheKey: cacheKey}
	got, err := client.GenerateRecipe(t.Context(), []string{"Cuisine direction for this recipe: Korean."}, menu)
	if err != nil {
		t.Fatalf("GenerateRecipe returned error: %v", err)
	}
	if got.ResponseID != "resp-recipe" || got.Title != "Korean Chicken" {
		t.Fatalf("unexpected recipe: %+v", got)
	}
	assert.Equal(t, 35, got.Properties.TotalMinutes)
	assert.Equal(t, []CookingMethod{CookingMethodStovetop}, got.Properties.CookingMethods)
	if !reflect.DeepEqual(got.Instructions, []string{"Prep."}) {
		t.Fatalf("unexpected instructions: %+v", got.Instructions)
	}
	if got.PromptCacheKey != cacheKey {
		t.Fatalf("expected recipe to retain prompt cache key %q, got %q", cacheKey, got.PromptCacheKey)
	}
	if strings.Contains(requestBody, "Chicken thighs") {
		t.Fatalf("recipe continuation should not resend ingredient TSV: %s", requestBody)
	}
	if !strings.Contains(requestBody, `"previous_response_id":"resp-menu-plan"`) {
		t.Fatalf("expected previous response id in request: %s", requestBody)
	}
	if !strings.Contains(requestBody, `"prompt_cache_key":"careme:store-day:v1:`) ||
		!strings.Contains(requestBody, `"prompt_cache_options":{"mode":"explicit","ttl":"30m"}`) {
		t.Fatalf("expected explicit prompt cache configuration: %s", requestBody)
	}
	if !strings.Contains(requestBody, "Cuisine direction for this recipe: Korean.") || !strings.Contains(requestBody, "professional chef and recipe developer") {
		t.Fatalf("expected recipe instructions and system prompt in request: %s", requestBody)
	}
	if recorder.record == nil || recorder.record.PreviousResponseID != "resp-menu-plan" {
		t.Fatalf("expected prompt record parent response ID, got %#v", recorder.record)
	}
}

func TestAskQuestionAddsExplicitCacheBreakpoint(t *testing.T) {
	var requestBody string
	client := NewClient("test-key", "ignored", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id":"resp-question","object":"response","created_at":1778529600,
				"status":"completed","model":%q,
				"output":[{"id":"msg-question","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Use half as much salt.","annotations":[]}]}],
				"usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":15},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":25}
			}`, defaultRecipeModel))),
			Request: req,
		}, nil
	})}, nil)

	answer, err := client.AskQuestion(t.Context(), "Can I reduce the salt?", ResponseRef{
		ID:             "resp-recipe",
		PromptCacheKey: "careme:store-day:v1:test",
	})
	if err != nil {
		t.Fatalf("AskQuestion returned error: %v", err)
	}
	if answer.ResponseID != "resp-question" || answer.Answer != "Use half as much salt." {
		t.Fatalf("unexpected answer: %+v", answer)
	}
	if !strings.Contains(requestBody, `"previous_response_id":"resp-recipe"`) ||
		!strings.Contains(requestBody, `"prompt_cache_options":{"mode":"explicit","ttl":"30m"}`) ||
		!strings.Contains(requestBody, `"prompt_cache_breakpoint":{"mode":"explicit"}`) {
		t.Fatalf("expected explicit question cache breakpoint: %s", requestBody)
	}
}

func TestResponseUsageLogAttr(t *testing.T) {
	attr := responseUsageLogAttr(defaultRecipeModel, responses.ResponseUsage{
		InputTokens:  1200,
		OutputTokens: 350,
		TotalTokens:  1550,
		InputTokensDetails: responses.ResponseUsageInputTokensDetails{
			CachedTokens:     900,
			CacheWriteTokens: 200,
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
		slog.Group("inputTokensDetails",
			slog.Int64("cachedTokens", 900),
			slog.Int64("cacheWriteTokens", 200),
		),
		slog.Int64("outputTokens", 350),
		slog.Group("outputTokensDetails", slog.Int64("reasoningTokens", 125)),
		slog.Int64("totalTokens", 1550),
		slog.Group("spend",
			slog.String("currency", "USD"),
			slog.Float64("totalUSD", 0.0127),
			slog.Float64("inputUSD", 0.0005),
			slog.Float64("cachedInputUSD", 0.00045),
			slog.Float64("cacheWriteInputUSD", 0.00125),
			slog.Float64("outputUSD", 0.0105),
		),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}
