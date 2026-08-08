package ai

import (
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
		"Set quantity to the total amount needed across the entire recipe",
		"Every time a step mentions an ingredient, including a pantry ingredient, state the exact amount of that ingredient used in that step.",
		"When an ingredient is divided among steps, the step amounts must add up to the total quantity in ingredients.",
		`Do not use an unquantified phrase such as "the remaining oil"`,
		"Cross-check every ingredient mention in the instructions for an exact step-level amount",
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

	cacheKey := storeDayPromptCacheKey("store-123", time.Date(2026, time.August, 4, 0, 0, 0, 0, time.UTC).Format("2006-01-02"))
	menu := ResponseRef{ID: "resp-menu-plan", PromptCacheKey: cacheKey}
	got, err := client.GenerateRecipe(t.Context(), []string{"Cuisine direction for this recipe: Korean."}, menu)
	if err != nil {
		t.Fatalf("GenerateRecipe returned error: %v", err)
	}
	if got.ResponseID != "resp-recipe" || got.Title != "Korean Chicken" {
		t.Fatalf("unexpected recipe: %+v", got)
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
