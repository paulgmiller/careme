package ai

import (
	"fmt"
	"slices"
	"testing"
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
	//fnv 128 url encoded is 24
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

func TestNormalizeWeeklyAdIngredients(t *testing.T) {
	got := normalizeWeeklyAdIngredients([]WeeklyAdIngredient{
		{PageNumber: 1, Name: " Strawberries ", Brand: "Safeway", Size: "1 lb", Price: "$2.99", SaleNotes: "member price"},
		{PageNumber: 1, Name: "Strawberries", Brand: "Safeway", Size: "1 lb", Price: "$2.99", SaleNotes: "member price"},
		{PageNumber: 1, Name: " ", Brand: "Ignored"},
		{PageNumber: 2, Name: "Avocado", SaleNotes: "4 for $5"},
	})

	want := []WeeklyAdIngredient{
		{PageNumber: 1, Name: "Strawberries", Brand: "Safeway", Size: "1 lb", Price: "$2.99", SaleNotes: "member price"},
		{PageNumber: 2, Name: "Avocado", SaleNotes: "4 for $5"},
	}

	if !slices.Equal(got, want) {
		t.Fatalf("unexpected normalized weekly ad ingredients: got %#v want %#v", got, want)
	}
}

func TestWeeklyAdSchemaRequiresAllIngredientFields(t *testing.T) {
	client := NewClient("test-key", "")

	properties, ok := client.adSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %#v", client.adSchema["properties"])
	}
	ingredients, ok := properties["ingredients"].(map[string]any)
	if !ok {
		t.Fatalf("ingredients schema missing or wrong type: %#v", properties["ingredients"])
	}
	items, ok := ingredients["items"].(map[string]any)
	if !ok {
		t.Fatalf("ingredient item schema missing or wrong type: %#v", ingredients["items"])
	}
	itemProperties, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("ingredient item properties missing or wrong type: %#v", items["properties"])
	}
	required, ok := items["required"].([]any)
	if !ok {
		t.Fatalf("ingredient item required missing or wrong type: %#v", items["required"])
	}

	requiredSet := make(map[string]struct{}, len(required))
	for _, entry := range required {
		requiredSet[fmt.Sprint(entry)] = struct{}{}
	}
	for key := range itemProperties {
		if _, ok := requiredSet[key]; !ok {
			t.Fatalf("ingredient item schema does not require %q", key)
		}
	}
}
