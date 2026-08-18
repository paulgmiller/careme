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

func TestRecipeStructuredInstructionHashUsesFlattenedContent(t *testing.T) {
	base := Recipe{InstructionsV2: []Instruction{{
		Phase:       1,
		Text:        "Prepare the sauce.",
		Ingredients: []string{"1 tablespoon olive oil"},
	}}}
	differentPhase := Recipe{InstructionsV2: []Instruction{{
		Phase:       2,
		Text:        "Prepare the sauce.",
		Ingredients: []string{"1 tablespoon olive oil"},
	}}}
	differentIngredient := Recipe{InstructionsV2: []Instruction{{
		Phase:       1,
		Text:        "Prepare the sauce.",
		Ingredients: []string{"2 tablespoons olive oil"},
	}}}

	if base.ComputeHash() != differentPhase.ComputeHash() {
		t.Fatal("phase-only changes should not change recipe hashes")
	}
	if base.ComputeHash() == differentIngredient.ComputeHash() {
		t.Fatal("nested ingredients should contribute to structured recipe hashes")
	}

	legacy := Recipe{Instructions: []string{base.InstructionsV2[0].flattenedText()}}
	if base.ComputeHash() != legacy.ComputeHash() {
		t.Fatal("structured and flattened legacy instructions should produce the same hash")
	}
}

func TestRecipeDecodesAndPreservesLegacyInstructions(t *testing.T) {
	const body = `{"title":"Soup","instructions":["Chop the onion.","Simmer the soup."]}`
	var recipe Recipe
	if err := json.Unmarshal([]byte(body), &recipe); err != nil {
		t.Fatalf("decode legacy recipe: %v", err)
	}
	structured := recipe.StructuredInstructions()
	if len(structured) != 2 || structured[0].Phase != 1 || structured[1].Phase != 2 {
		t.Fatalf("unexpected legacy phases: %+v", structured)
	}
	if structured[0].Text != "Chop the onion." {
		t.Fatalf("unexpected legacy instruction: %+v", structured[0])
	}

	encoded, err := json.Marshal(recipe)
	if err != nil {
		t.Fatalf("encode legacy recipe: %v", err)
	}
	if !strings.Contains(string(encoded), `"instructions":["Chop the onion.","Simmer the soup."]`) {
		t.Fatalf("legacy instructions should retain their wire shape: %s", encoded)
	}
}

func TestRecipeStructuredInstructionsPanicsWhenBothVersionsAreSet(t *testing.T) {
	recipe := Recipe{
		Instructions: []string{"Legacy step."},
		InstructionsV2: []Instruction{{
			Phase: 1,
			Text:  "Structured step.",
		}},
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected both instruction versions to panic")
		}
	}()
	recipe.StructuredInstructions()
}

func TestRecipeSerializesOnlyV2Instructions(t *testing.T) {
	recipe := Recipe{InstructionsV2: []Instruction{{
		Phase:       1,
		Text:        "Prepare the vegetables:",
		Ingredients: []string{"1 pepper, diced", "2 onions, sliced", "3 garlic cloves, minced"},
	}}}
	encoded, err := json.Marshal(recipe)
	if err != nil {
		t.Fatalf("encode structured recipe: %v", err)
	}
	if strings.Contains(string(encoded), `"instructions":`) {
		t.Fatalf("structured recipe JSON should omit legacy instructions: %s", encoded)
	}
	want := `"instructionsv2":[{"phase":1,"text":"Prepare the vegetables:","ingredients":["1 pepper, diced","2 onions, sliced","3 garlic cloves, minced"]}]`
	if !strings.Contains(string(encoded), want) {
		t.Fatalf("structured recipe JSON should contain %s: %s", want, encoded)
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

func TestRecipeSchemaUsesStructuredInstructions(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, nil)
	properties := schemaProperties(t, client.recipeSchema)
	if _, ok := properties["instructions"]; ok {
		t.Fatal("legacy instructions should not be part of the model schema")
	}
	instructions := schemaObject(t, properties["instructionsv2"])
	items := schemaObject(t, instructions["items"])
	instructionProperties := schemaProperties(t, items)
	required := schemaRequired(t, items)

	for _, field := range []string{"phase", "text", "ingredients"} {
		if _, ok := instructionProperties[field]; !ok {
			t.Fatalf("expected instruction schema to contain %q", field)
		}
		if !slices.Contains(required, field) {
			t.Fatalf("expected instruction schema to require %q, got %v", field, required)
		}
	}
	phase := schemaObject(t, instructionProperties["phase"])
	if phase["minimum"] != float64(1) {
		t.Fatalf("expected phase minimum 1, got %#v", phase["minimum"])
	}
}

func TestValidateRecipeInstructions(t *testing.T) {
	tests := []struct {
		name         string
		instructions []Instruction
		wantError    string
	}{
		{
			name: "parallel phases",
			instructions: []Instruction{
				{Phase: 1, Text: "Prep."},
				{Phase: 2, Text: "Cook the rice."},
				{Phase: 2, Text: "Roast the vegetables."},
				{Phase: 3, Text: "Plate."},
			},
		},
		{name: "empty", wantError: "at least one instruction is required"},
		{name: "phase zero", instructions: []Instruction{{Text: "Prep."}}, wantError: "phase must be positive"},
		{name: "does not start at one", instructions: []Instruction{{Phase: 2, Text: "Prep."}}, wantError: "must start at 1"},
		{name: "decreasing", instructions: []Instruction{{Phase: 1, Text: "Prep."}, {Phase: 2, Text: "Cook."}, {Phase: 1, Text: "Plate."}}, wantError: "must not decrease"},
		{name: "skipped", instructions: []Instruction{{Phase: 1, Text: "Prep."}, {Phase: 3, Text: "Cook."}}, wantError: "skips phase 2"},
		{name: "empty text", instructions: []Instruction{{Phase: 1, Text: "  "}}, wantError: "text is required"},
		{name: "empty ingredient", instructions: []Instruction{{Phase: 1, Text: "Prep.", Ingredients: []string{" "}}}, wantError: "ingredient 1 is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecipeInstructions(tt.instructions)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validate instructions: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func TestInstructionRejectsNegativePhaseDuringDecoding(t *testing.T) {
	var instruction Instruction
	err := json.Unmarshal([]byte(`{"phase":-1,"text":"Prep.","ingredients":[]}`), &instruction)
	if err == nil {
		t.Fatal("negative phase should not decode into an unsigned phase")
	}
}

func TestSystemMessageRequiresPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"start with prep such as preheating, chopping, slicing, dicing, mixing, or make-ahead work before active cooking",
		"do not rely on prep details from the ingredient list alone",
		"Legacy instructions are read-only compatibility data; return only instructionsv2.",
		"provide the total elapsed recipe time",
		"5 to 8 clear steps",
		"Each step should cover one coherent task or component.",
		"Keep immediate actions on the same ingredient together, such as patting shrimp dry and seasoning it.",
		"if they can happen concurrently, give those separate steps the same phase instead of combining them",
		"Steps with the same phase can be done concurrently.",
		"use this nested list only when the cook must measure, prepare, or combine at least three distinct ingredients in this step",
		"Do not use a nested list for ordinary cooking actions, resting, serving, plating, a single primary ingredient",
		"later steps should refer to that component by name without restating its ingredients or their amounts",
		"Ensure cook_time reflects the total elapsed time implied by every instruction step, including prep, resting, passive cooking, and work performed in parallel phases",
		"set id to the exact ProductId",
		"Set quantity to the total amount needed across the entire recipe",
		"Every time a step first uses an ingredient, including a pantry ingredient, state its exact amount in either the step text or that step's ingredients list.",
		"When an ingredient is divided among steps, the step amounts must add up to the total quantity in ingredients.",
		`Do not use an unquantified phrase such as "the remaining oil"`,
		"Cross-check every ingredient mention in instruction text and nested ingredient lists for an exact step-level amount",
		"Presalting meat and salting pasta or blanching water season food during cooking.",
		"Do not reduce or omit those applications merely because salt or salty ingredients are added later; adjust finishing salt instead.",
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
						"text": "{\"title\":\"Korean Chicken\",\"description\":\"Fast dinner.\",\"cook_time\":\"35 minutes\",\"cost_estimate\":\"$12\",\"ingredients\":[],\"instructionsv2\":[{\"phase\":1,\"text\":\"Prep.\",\"ingredients\":[]}],\"health\":\"Balanced.\",\"drink_pairing\":\"Water.\",\"wine_styles\":[]}",
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
	if len(got.Instructions) != 0 || len(got.InstructionsV2) != 1 || got.InstructionsV2[0].Text != "Prep." {
		t.Fatalf("expected only v2 instructions, got %+v", got)
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
