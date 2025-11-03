package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func isValidHTML(t *testing.T, htmlStr string) {
	if htmlStr == "" {
		t.Fatal("rendered HTML is empty")
	}
	_, err := html.Parse(bytes.NewBufferString(htmlStr))
	if err != nil {
		t.Fatalf("rendered HTML is not valid: %v\nHTML:\n%s", err, htmlStr)
	}
}

var list = ai.ShoppingList{
	Recipes: []ai.Recipe{
		{
			Title:       "Test Recipe",
			Description: "A simple test recipe",
			Ingredients: []ai.Ingredient{
				{Name: "Ingredient 1", Quantity: "1 cup", Price: "2.00"},
				{Name: "Ingredient 2", Quantity: "2 tbsp", Price: "1.50"},
			},
			Instructions: []string{
				"Step 1: Do something.",
				"Step 2: Do something else.",
			},
			Health:       "Healthy",
			DrinkPairing: "Water",
		},
	},
}

func TestFormatChatHTML_ValidHTML(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: "test123"},
		},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var buf bytes.Buffer
	if err := g.FormatChatHTML(p, list, &buf); err != nil {
		t.Fatalf("failed to format chat HTML: %v", err)
	}
	html := buf.String()
	isValidHTML(t, html)
}

func TestFormatChatHTML_IncludesClarityScript(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: "test456"},
		},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var buf bytes.Buffer
	if err := g.FormatChatHTML(p, list, &buf); err != nil {
		t.Fatalf("failed to format chat HTML: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains(buf.Bytes(), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatChatHTML_NoClarityWhenEmpty(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: ""},
		},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var buf bytes.Buffer
	if err := g.FormatChatHTML(p, list, &buf); err != nil {
		t.Fatalf("failed to format chat HTML: %v", err)
	}

	if bytes.Contains(buf.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatIngredientsHTML_ValidHTML(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: "test123"},
		},
	}
	recipe := &ai.Recipe{
		Title:       "Test Recipe",
		Description: "A simple test recipe",
		Ingredients: []ai.Ingredient{
			{Name: "Ingredient 1", Quantity: "1 cup", Price: "$2.00"},
			{Name: "Ingredient 2", Quantity: "2 tbsp", Price: "$1.50"},
		},
		Instructions: []string{
			"Step 1: Do something.",
			"Step 2: Do something else.",
		},
		Health:       "Healthy",
		DrinkPairing: "Water",
	}
	var buf bytes.Buffer
	if err := g.FormatIngredientsHTML(recipe, &buf); err != nil {
		t.Fatalf("failed to format ingredients HTML: %v", err)
	}
	html := buf.String()
	isValidHTML(t, html)
}

func TestFormatIngredientsHTML_ContainsIngredients(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: ""},
		},
	}
	recipe := &ai.Recipe{
		Title:       "Pasta Carbonara",
		Description: "Classic Italian pasta dish",
		Ingredients: []ai.Ingredient{
			{Name: "Spaghetti", Quantity: "200g", Price: "$3.00"},
			{Name: "Eggs", Quantity: "2", Price: "$1.00"},
		},
	}
	var buf bytes.Buffer
	if err := g.FormatIngredientsHTML(recipe, &buf); err != nil {
		t.Fatalf("failed to format ingredients HTML: %v", err)
	}

	// Check that title is present
	if !bytes.Contains(buf.Bytes(), []byte("Pasta Carbonara")) {
		t.Error("HTML should contain recipe title")
	}

	// Check that ingredients are present
	if !bytes.Contains(buf.Bytes(), []byte("Spaghetti")) {
		t.Error("HTML should contain ingredient name")
	}

	if !bytes.Contains(buf.Bytes(), []byte("200g")) {
		t.Error("HTML should contain ingredient quantity")
	}

	// Check that it contains a link back to the full recipe
	if !bytes.Contains(buf.Bytes(), []byte("/recipe/")) {
		t.Error("HTML should contain link to full recipe")
	}
}

