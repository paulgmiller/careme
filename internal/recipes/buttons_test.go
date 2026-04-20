package recipes

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/stretchr/testify/require"
)

// Test that the HTML contains Save and Dismiss buttons for recipes.
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

	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var w bytes.Buffer
	err := FormatShoppingListHTML(t.Context(), p, multiRecipeList, true, &w)
	require.NoError(t, err)
	html := w.String()

	// Verify HTML is valid
	isValidHTML(t, html)

	// Check for radio buttons
	if !strings.Contains(html, `type="radio"`) {
		t.Error("HTML should contain radio button inputs")
	}
	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/save"`) {
		t.Error("HTML should contain HTMX save action")
	}
	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/dismiss"`) {
		t.Error("HTML should contain HTMX dismiss action")
	}
	if !strings.Contains(html, `hx-trigger="click"`) {
		t.Error("HTML should trigger HTMX requests on click")
	}

	// Check for Save and Dismiss labels.
	if !strings.Contains(html, `Save`) {
		t.Error("HTML should contain Save label text")
	}
	if !strings.Contains(html, `Dismis`) {
		t.Error("HTML should contain Dismiss label text")
	}
	if !strings.Contains(html, `Details`) {
		t.Error("HTML should contain Details button text")
	}

	// Check that "Try again, chef" button exists
	if !strings.Contains(html, `Try again, chef`) {
		t.Error("HTML should contain Try again, chef button")
	}

	// Check that "Save my picks" button exists.
	if !strings.Contains(html, `Assemble Shopping List`) {
		t.Error("HTML should contain Assemble Shopping List button")
	}
	if !strings.Contains(html, `disabled`) {
		t.Error("HTML should disable finalize button when no recipes are saved")
	}
	if strings.Contains(html, `/finalize"`) {
		t.Error("HTML should not wire finalize endpoint when button is disabled")
	}
	if !strings.Contains(html, `Save at least one recipe to assemble your shopping list.`) {
		t.Error("HTML should explain how to enable finalize button")
	}

	if !strings.Contains(html, `/recipes/`) || !strings.Contains(html, `/regenerate"`) {
		t.Error("HTML should submit regenerate with POST endpoint")
	}
	if !strings.Contains(html, `hx-params="instructions"`) {
		t.Error("HTML regenerate form should submit only instructions")
	}

	// Check that recipes are present with their titles
	if !strings.Contains(html, "Recipe One") {
		t.Error("HTML should contain Recipe One title")
	}
	if !strings.Contains(html, "Recipe Two") {
		t.Error("HTML should contain Recipe Two title")
	}
}

func TestFormatShoppingListHTML_EnablesFinalizeWhenRecipeSaved(t *testing.T) {
	listWithSavedRecipe := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:       "Saved Recipe",
				Description: "Saved recipe",
				Ingredients: []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"}},
				Instructions: []string{
					"Step 1",
				},
				Health:       "Healthy",
				DrinkPairing: "Water",
			},
		},
	}

	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.Saved = []ai.Recipe{listWithSavedRecipe.Recipes[0]}
	var w bytes.Buffer
	err := FormatShoppingListHTML(t.Context(), p, listWithSavedRecipe, true, &w)
	require.NoError(t, err)
	html := w.String()

	if !strings.Contains(html, `hx-post="/recipes/`) || !strings.Contains(html, `/finalize"`) {
		t.Error("HTML should submit finalize with HTMX POST when a recipe is saved")
	}
	if strings.Contains(html, `id="finalize-help"`) {
		t.Error("HTML should not render finalize helper text when button is enabled")
	}
}

func TestFormatShoppingListHTML_SignedOutShowsReadOnlyActions(t *testing.T) {
	list := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:        "Shared Recipe",
				Description:  "Read-only recipe",
				Ingredients:  []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"}},
				Instructions: []string{"Step 1"},
				Health:       "Healthy",
				DrinkPairing: "Water",
			},
		},
	}

	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var w bytes.Buffer
	err := FormatShoppingListHTML(t.Context(), p, list, false, &w)
	require.NoError(t, err)
	html := w.String()

	if strings.Contains(html, `type="radio"`) {
		t.Error("HTML should not contain save/dismiss radio inputs when signed out")
	}
	if strings.Contains(html, `Try again, chef`) {
		t.Error("HTML should not contain regenerate action text when signed out")
	}
	if strings.Contains(html, `Assemble Shopping List`) {
		t.Error("HTML should not contain finalize action text when signed out")
	}
	if strings.Contains(html, `Save`) {
		t.Error("HTML should not contain save action text when signed out")
	}
	if strings.Contains(html, `Dismiss`) {
		t.Error("HTML should not contain dismiss action text when signed out")
	}
}
