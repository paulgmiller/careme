package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"strings"
	"testing"
	"time"
)

// Test that the HTML contains Save and Dismiss buttons for recipes
func TestFormatChatHTML_ContainsSaveAndDismissButtons(t *testing.T) {
	g := Generator{
		config: &config.Config{
			Clarity: config.ClarityConfig{ProjectID: ""},
		},
	}

	// Create a shopping list with multiple recipes
	multiRecipeList := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:       "Recipe One",
				Description: "First recipe",
				Ingredients: []ai.Ingredient{
					{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"},
				},
				Instructions: []string{"Step 1"},
				Health:       "Healthy",
				DrinkPairing: "Water",
			},
			{
				Title:       "Recipe Two",
				Description: "Second recipe",
				Ingredients: []ai.Ingredient{
					{Name: "ingredient2", Quantity: "2 cups", Price: "3.00"},
				},
				Instructions: []string{"Step 1"},
				Health:       "Very Healthy",
				DrinkPairing: "Tea",
			},
		},
	}

	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var buf bytes.Buffer
	if err := g.FormatChatHTML(p, multiRecipeList, &buf); err != nil {
		t.Fatalf("failed to format chat HTML: %v", err)
	}

	html := buf.String()

	// Verify HTML is valid
	isValidHTML(t, html)

	// Check for Save checkboxes
	if !strings.Contains(html, `name="saved"`) {
		t.Error("HTML should contain saved checkbox inputs")
	}
	if !strings.Contains(html, `<span>Save</span>`) {
		t.Error("HTML should contain Save label")
	}

	// Check for Dismiss checkboxes
	if !strings.Contains(html, `name="dismissed"`) {
		t.Error("HTML should contain dismissed checkbox inputs")
	}
	if !strings.Contains(html, `<span>Dismiss</span>`) {
		t.Error("HTML should contain Dismiss label")
	}

	// Check that checkboxes have recipe hash values
	if !strings.Contains(html, `type="checkbox"`) {
		t.Error("HTML should contain checkbox inputs")
	}

	// Check that "Regenerate" button exists
	if !strings.Contains(html, `Regenerate`) {
		t.Error("HTML should contain Regenerate button")
	}

	// Check that recipes are present with their titles
	if !strings.Contains(html, "Recipe One") {
		t.Error("HTML should contain Recipe One title")
	}
	if !strings.Contains(html, "Recipe Two") {
		t.Error("HTML should contain Recipe Two title")
	}
}
