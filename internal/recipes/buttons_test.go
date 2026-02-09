package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Test that the HTML contains Save and Dismiss buttons for recipes
func TestFormatShoppingListHTML_ContainsSaveAndDismissButtons(t *testing.T) {
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
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, multiRecipeList, true, w)
	html := w.Body.String()

	// Verify HTML is valid
	isValidHTML(t, html)

	// Check for Save and Dismiss radio buttons and labels
	if !strings.Contains(html, `name="saved"`) {
		t.Error("HTML should contain saved hidden inputs")
	}
	if !strings.Contains(html, `name="dismissed"`) {
		t.Error("HTML should contain dismissed hidden inputs")
	}

	// Check for radio buttons
	if !strings.Contains(html, `type="radio"`) {
		t.Error("HTML should contain radio button inputs")
	}

	// Check for Save and Dismiss labels (without span tags)
	if !strings.Contains(html, `Save`) {
		t.Error("HTML should contain Save label text")
	}
	if !strings.Contains(html, `Dismiss`) {
		t.Error("HTML should contain Dismiss label text")
	}
	if !strings.Contains(html, `Details`) {
		t.Error("HTML should contain Details button text")
	}

	// Check that "Regenerate" button exists
	if !strings.Contains(html, `Regenerate`) {
		t.Error("HTML should contain Regenerate button")
	}

	// Check that "Finalize" button exists
	if !strings.Contains(html, `Finalize`) {
		t.Error("HTML should contain Finalize button")
	}

	// Check for finalize submit button (not a POST form anymore)
	if !strings.Contains(html, `name="finalize"`) {
		t.Error("HTML should have finalize submit button")
	}
	if !strings.Contains(html, `value="true"`) {
		t.Error("HTML should have finalize value set to true")
	}

	// Check that recipes are present with their titles
	if !strings.Contains(html, "Recipe One") {
		t.Error("HTML should contain Recipe One title")
	}
	if !strings.Contains(html, "Recipe Two") {
		t.Error("HTML should contain Recipe Two title")
	}
}
