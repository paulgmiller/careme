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

func TestPrepareRecipeContextStoresMinimalOutputResponse(t *testing.T) {
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
				"id": "resp-shared-context",
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [],
				"usage": {
					"input_tokens": 120,
					"input_tokens_details": {"cached_tokens": 0},
					"output_tokens": 0,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 120
				}
			}`, defaultRecipeModel))),
			Request: req,
		}, nil
	})}, recorder)

	price := float32(8.99)
	got, err := client.PrepareRecipeContext(t.Context(), &locationtypes.Location{State: "WA"}, []InputIngredient{
		{ProductID: "chicken-1", Description: "Chicken thighs", Size: "2 lb", PriceRegular: &price},
	}, time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC), []string{"Lemon pasta"})
	if err != nil {
		t.Fatalf("PrepareRecipeContext returned error: %v", err)
	}
	if got.ResponseID != "resp-shared-context" {
		t.Fatalf("unexpected recipe context: %#v", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(requestBody), &body); err != nil {
		t.Fatalf("unmarshal request body: %v\n%s", err, requestBody)
	}
	if body["max_output_tokens"] != float64(16) {
		t.Fatalf("expected minimal max output tokens, got %#v in %s", body["max_output_tokens"], requestBody)
	}
	if body["store"] != true {
		t.Fatalf("expected stored response, got %s", requestBody)
	}
	if _, ok := body["text"]; ok {
		t.Fatalf("context warmup should not request recipe JSON schema: %s", requestBody)
	}
	if !strings.Contains(requestBody, "Chicken thighs") || !strings.Contains(requestBody, "Default: each recipe should serve 2 people.") {
		t.Fatalf("expected shared ingredient and serving context in request: %s", requestBody)
	}
	if strings.Contains(requestBody, "Use sale ingredients.") {
		t.Fatalf("recipe context should not receive planner instructions: %s", requestBody)
	}
	if !strings.Contains(requestBody, "professional chef and recipe developer") || !strings.Contains(requestBody, prepareRecipeContextInstruction) {
		t.Fatalf("expected recipe system prompt and context seed instruction in request: %s", requestBody)
	}
	if recorder.record == nil || recorder.record.ResponseID != "resp-shared-context" {
		t.Fatalf("expected context prompt record, got %#v", recorder.record)
	}
}

func TestGenerateRecipeFromContextUsesPreviousResponseWithoutIngredientTSV(t *testing.T) {
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
						"text": "{\"title\":\"Korean Chicken\",\"description\":\"Fast dinner.\",\"cook_time\":\"35 minutes\",\"cost_estimate\":\"$12\",\"ingredients\":[],\"instructions\":[\"Prep.\"],\"health\":\"Balanced.\",\"drink_pairing\":\"Water.\",\"wine_styles\":[]}",
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

	got, err := client.GenerateRecipeFromContext(t.Context(), []string{"Cuisine direction for this recipe: Korean."}, RecipeContext{ResponseID: "resp-shared-context"})
	if err != nil {
		t.Fatalf("GenerateRecipeFromContext returned error: %v", err)
	}
	if got.ResponseID != "resp-recipe" || got.Title != "Korean Chicken" {
		t.Fatalf("unexpected recipe: %+v", got)
	}
	if strings.Contains(requestBody, "Chicken thighs") {
		t.Fatalf("recipe continuation should not resend ingredient TSV: %s", requestBody)
	}
	if !strings.Contains(requestBody, `"previous_response_id":"resp-shared-context"`) {
		t.Fatalf("expected previous response id in request: %s", requestBody)
	}
	if !strings.Contains(requestBody, "Cuisine direction for this recipe: Korean.") || !strings.Contains(requestBody, "professional chef and recipe developer") {
		t.Fatalf("expected recipe instructions and system prompt in request: %s", requestBody)
	}
	if recorder.record == nil || recorder.record.PreviousResponseID != "resp-shared-context" {
		t.Fatalf("expected prompt record parent response ID, got %#v", recorder.record)
	}
}

func TestResponseUsageLogAttr(t *testing.T) {
	attr := responseUsageLogAttr(defaultRecipeModel, responses.ResponseUsage{
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
		slog.Group("spend",
			slog.String("currency", "USD"),
			slog.Float64("totalUSD", 0.01245),
			slog.Float64("inputUSD", 0.0015),
			slog.Float64("cachedInputUSD", 0.00045),
			slog.Float64("outputUSD", 0.0105),
		),
	}) {
		t.Fatalf("unexpected attrs: %#v", attr.Value.Group())
	}
}
