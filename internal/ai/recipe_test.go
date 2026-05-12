package ai

import (
	"encoding/json"
	"log/slog"
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

	recipe.Saved = true
	recipe.OriginHash = "somehashvalue"

	// Hash should be consistent regardless of silly fields
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
		{ProductID: "pinot-noir-1", Description: "Pinot Noir", Size: "750mL", PriceRegular: float32Ptr(13.99)},
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
	client := NewClient("test-key", "ignored", nil, &cachePromptRecorder{})
	location := &locationtypes.Location{State: "WA"}
	ingredients := []InputIngredient{
		{ProductID: "chicken-1", Description: "Chicken thighs", Size: "2 lb", PriceRegular: float32Ptr(8.99)},
		{ProductID: "beans-1", Description: "Green beans", Size: "12 oz", PriceRegular: float32Ptr(2.99)},
	}
	instructions := []string{"make it high protein"}
	lastRecipes := []string{"Lemon chicken pasta"}
	date := time.Date(2026, time.May, 11, 0, 0, 0, 0, time.UTC)

	contextMessages, err := client.buildRecipeContextMessages(location, ingredients, instructions, date, lastRecipes)
	if err != nil {
		t.Fatalf("buildRecipeContextMessages returned error: %v", err)
	}
	menuMessages, err := client.buildMenuPlanMessages(location, ingredients, instructions, date, lastRecipes)
	if err != nil {
		t.Fatalf("buildMenuPlanMessages returned error: %v", err)
	}
	recipeMessages, err := client.buildRecipeMessages(location, ingredients, instructions, date, lastRecipes, RecipePlan{
		Cuisine:          "Korean",
		AnchorIngredient: "chicken thighs",
		Technique:        "stir-fry",
	})
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(body)
}

func TestSystemMessageRequiresPrepFirstAndTotalTiming(t *testing.T) {
	for _, want := range []string{
		"the first steps must be preparation steps before any cooking begins",
		"provide the total elapsed recipe time",
		"Ensure the first instructions say what prep can be done ahead of time.",
		"Ensure cook_time reflects the total time implied by every instruction step, including prep, resting, and passive cooking time.",
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
