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

	// Check for Save buttons
	if !strings.Contains(html, `class="save-btn`) {
		t.Error("HTML should contain Save button class")
	}
	// The onclick should contain saveRecipe with a hash parameter
	if !strings.Contains(html, `onclick="saveRecipe('`) {
		t.Error("HTML should contain saveRecipe calls with hash parameters")
	}

	// Check for Dismiss buttons
	if !strings.Contains(html, `class="dismiss-btn`) {
		t.Error("HTML should contain Dismiss button class")
	}
	// The onclick should contain dismissRecipe with a hash parameter
	if !strings.Contains(html, `onclick="dismissRecipe('`) {
		t.Error("HTML should contain dismissRecipe calls with hash parameters")
	}

	// Check for hidden form fields
	if !strings.Contains(html, `id="savedRecipes"`) {
		t.Error("HTML should contain savedRecipes hidden field")
	}
	if !strings.Contains(html, `id="dismissedRecipes"`) {
		t.Error("HTML should contain dismissedRecipes hidden field")
	}
	if !strings.Contains(html, `name="saved"`) {
		t.Error("HTML should contain saved field name")
	}
	if !strings.Contains(html, `name="dismissed"`) {
		t.Error("HTML should contain dismissed field name")
	}

	// Check for JavaScript functions
	if !strings.Contains(html, `function saveRecipe(hash)`) {
		t.Error("HTML should contain saveRecipe function with hash parameter")
	}
	if !strings.Contains(html, `function dismissRecipe(hash)`) {
		t.Error("HTML should contain dismissRecipe function with hash parameter")
	}
	if !strings.Contains(html, `const savedRecipes = new Set()`) {
		t.Error("HTML should contain savedRecipes Set")
	}
	if !strings.Contains(html, `const dismissedRecipes = new Set()`) {
		t.Error("HTML should contain dismissedRecipes Set")
	}

	// Check that "Regenerate" button exists
	if !strings.Contains(html, `Regenerate`) {
		t.Error("HTML should contain Regenerate button")
	}

	// Check for data attributes on recipe cards
	if !strings.Contains(html, `data-recipe-hash="`) {
		t.Error("HTML should contain data-recipe-hash attribute")
	}
	if !strings.Contains(html, `data-recipe-title="Recipe One"`) {
		t.Error("HTML should contain data-recipe-title for Recipe One")
	}
	if !strings.Contains(html, `data-recipe-title="Recipe Two"`) {
		t.Error("HTML should contain data-recipe-title for Recipe Two")
	}
}
