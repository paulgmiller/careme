package recipes

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
)

// Test that the HTML contains the simplified recipe action and live shopping list state.
func TestFormatShoppingListHTML_ContainsSaveActionAndEmptyListState(t *testing.T) {
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
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, multiRecipeList, true, w)
	html := w.Body.String()

	// Verify HTML is valid
	isValidHTML(t, html)

	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/save"`) {
		t.Error("HTML should contain HTMX save action")
	}
	if strings.Contains(html, `/dismiss"`) {
		t.Error("HTML should not render dismiss actions before a recipe is saved")
	}

	// Check for primary action labels.
	if !strings.Contains(html, `Save`) {
		t.Error("HTML should contain Save label text")
	}
	if !strings.Contains(html, `Details`) {
		t.Error("HTML should contain Details button text")
	}

	// Check that "Try again, chef" button exists
	if !strings.Contains(html, `Try again, chef`) {
		t.Error("HTML should contain Try again, chef button")
	}

	if strings.Contains(html, `/finalize"`) {
		t.Error("HTML should not render set-the-menu endpoint when nothing is saved")
	}
	if !strings.Contains(html, `Save a recipe to start your shopping list.`) {
		t.Error("HTML should explain how to start the live shopping list")
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

func TestFormatShoppingListHTML_ShowsSetMenuWhenSavedAndPendingRecipesRemain(t *testing.T) {
	listWithRecipes := ai.ShoppingList{
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
			{
				Title:        "Pending Recipe",
				Description:  "Waiting recipe",
				Ingredients:  []ai.Ingredient{{Name: "ingredient2", Quantity: "2 cups", Price: "4.00"}},
				Instructions: []string{"Step 2"},
				Health:       "Healthy",
				DrinkPairing: "Tea",
			},
		},
	}

	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.Saved = []ai.Recipe{listWithRecipes.Recipes[0]}
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, listWithRecipes, true, w)
	html := w.Body.String()

	if !strings.Contains(html, `hx-post="/recipes/`) || !strings.Contains(html, `/finalize"`) {
		t.Error("HTML should submit set-the-menu with HTMX POST when saved and pending recipes coexist")
	}
	if !strings.Contains(html, `Set the menu`) {
		t.Error("HTML should render the Set the menu label when saved and pending recipes coexist")
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
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, false, w)
	html := w.Body.String()

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
