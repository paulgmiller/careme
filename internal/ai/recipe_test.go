package ai

import (
	"slices"
	"strings"
	"testing"

	"careme/internal/kroger"
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
	if !strings.Contains(prompt, "Recipe:\nRoast Chicken\nCrisp skin and herbs.\nInstructions:\nRoast until golden.\n") {
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
	if !strings.Contains(prompt, "Title: Roast Chicken\nDescription: Crisp skin and herbs.\nCook time: 45 minutes\nCost estimate: $18-24\n") {
		t.Fatalf("expected recipe summary in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Existing drink pairing note: Pinot Noir\nSuggested wine styles: Pinot Noir, Chardonnay\nHealth notes: Balanced dinner\n") {
		t.Fatalf("expected pairing hints in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "- 1 whole Chicken ($12)\n- 1 Lemon ($1)\n") {
		t.Fatalf("expected ingredient replay in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "- Roast until golden.\n- Finish with lemon juice.\n") {
		t.Fatalf("expected instructions replay in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Candidate wines TSV:\nProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\n\t\t\tPinot Noir\t750mL\t13.99\t13.99\n") {
		t.Fatalf("expected candidate wines TSV in prompt: %s", prompt)
	}
}

func strPtr(s string) *string {
	return &s
}

func float32Ptr(v float32) *float32 {
	return &v
}
