package ai

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"careme/internal/kroger"
	"careme/internal/locations"
	openai "github.com/openai/openai-go/v3"
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
	wines := []kroger.Ingredient{
		{Description: strPtr("Pinot Noir"), Size: strPtr("750mL"), PriceRegular: float32Ptr(13.99)},
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
	if !strings.Contains(prompt, "Candidate wines TSV:\nProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\n\t\t\tPinot Noir\t750mL\t13.99\t13.99\n") {
		t.Fatalf("expected candidate wines TSV in prompt: %s", prompt)
	}
}

func TestRecipeModelOrDefault(t *testing.T) {
	if got := recipeModelOrDefault(""); got != openai.ChatModelGPT5_4 {
		t.Fatalf("empty model should fall back to gpt-5.4, got %q", got)
	}
	if got := recipeModelOrDefault("gpt-5.4-mini"); got != "gpt-5.4-mini" {
		t.Fatalf("explicit model should be preserved, got %q", got)
	}
}

func TestBuildRecipeDefaultsMessage(t *testing.T) {
	location := &locations.Location{State: "CA"}
	date := time.Date(2026, time.April, 2, 0, 0, 0, 0, time.UTC)

	got := buildRecipeDefaultsMessage(location, date)

	for _, want := range []string{
		"Fallback defaults for recipe generation. Use these only when the user has not already specified otherwise.",
		"follow the user's instruction and ignore the default",
		"Current date and state for seasonal tie-breakers: April 2, 2026 in CA.",
		"Seasonal ingredients are a tie-breaker for freshness and value only when they fit the requested cuisine and dish naturally.",
		"Default servings: 2 people.",
		"Default recipe count: 3 recipes.",
		"Default prep and cook time: under 1 hour.",
		"Default cooking methods: oven, stove, grill, slow cooker.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("defaults message missing %q:\n%s", want, got)
		}
	}
}

func TestBuildRecipeMessagesUsesConsolidatedFallbackDefaults(t *testing.T) {
	client := NewClient("test-key", "")
	location := &locations.Location{State: "CA"}
	date := time.Date(2026, time.April, 2, 0, 0, 0, 0, time.UTC)
	ingredients := []kroger.Ingredient{
		{Description: strPtr("Asparagus"), Size: strPtr("1 lb"), PriceRegular: float32Ptr(3.99)},
	}

	messages, err := client.buildRecipeMessages(location, ingredients, []string{"Serve 4 people", "Make it Greek"}, date, nil)
	if err != nil {
		t.Fatalf("buildRecipeMessages returned error: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected defaults, ingredients, and two user instruction messages; got %d", len(messages))
	}

	first, err := json.Marshal(messages[0])
	if err != nil {
		t.Fatalf("failed to marshal first message: %v", err)
	}
	firstMessage := string(first)
	if !strings.Contains(firstMessage, "Fallback defaults for recipe generation") {
		t.Fatalf("expected first message to contain fallback defaults block: %s", firstMessage)
	}
	if strings.Contains(firstMessage, "Prioritize ingredients that are in season for the current date and user's state location") {
		t.Fatalf("expected seasonality to live inside fallback defaults block instead of a separate binding instruction: %s", firstMessage)
	}
	if strings.Contains(firstMessage, "\"Default: each recipe should serve 2 people.\"") {
		t.Fatalf("expected old per-default binding message to be removed: %s", firstMessage)
	}
}

func TestSystemMessagePrioritizesUserIntentAndVerification(t *testing.T) {
	for _, want := range []string{
		"Follow constraints in this order: explicit user instructions and corrections, factual input data, fallback defaults, then stylistic goals.",
		"If seasonality conflicts with the requested cuisine, dish style, diet, budget, or other direct user intent, preserve the user's intent and cuisine coherence.",
		"Determine servings from explicit user instructions first; only use fallback defaults if the user did not specify servings.",
		"Do not invent prices.",
		"If exact nutrition is uncertain, keep calorie and macro guidance approximate and conservative.",
	} {
		if !strings.Contains(systemMessage, want) {
			t.Fatalf("system message missing %q", want)
		}
	}
}

func strPtr(s string) *string {
	return &s
}

func float32Ptr(v float32) *float32 {
	return &v
}
