package recipes

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
)

func TestFormatShoppingListHTML_ContainsAddHideAndDetailsButtons(t *testing.T) {
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
	FormatShoppingListHTMLForHash(t.Context(), p, multiRecipeList, nil, true, p.Hash(), nil, w)
	html := assertHTTPSuccess(t, w)

	// Verify HTML is valid
	isValidHTML(t, html)

	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/save"`) {
		t.Error("HTML should contain HTMX save action")
	}
	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/dismiss"`) {
		t.Error("HTML should contain HTMX dismiss action")
	}

	if !strings.Contains(html, `Add`) {
		t.Error("HTML should contain Add label text")
	}
	if !strings.Contains(html, `Hide`) {
		t.Error("HTML should contain Hide label text")
	}
	if !strings.Contains(html, `Details`) {
		t.Error("HTML should contain Details button text")
	}
	if strings.Contains(html, `<details open`) {
		t.Error("recipe details should start collapsed")
	}

	// Check that "Try again, chef" button exists
	if !strings.Contains(html, `Try again, chef`) {
		t.Error("HTML should contain Try again, chef button")
	}

	// Check that save list button exists.
	if !strings.Contains(html, `Save list`) {
		t.Error("HTML should contain Save list button")
	}
	if !strings.Contains(html, `disabled`) {
		t.Error("HTML should disable finalize button when no recipes are saved")
	}
	if strings.Contains(html, `/finalize"`) {
		t.Error("HTML should not wire finalize endpoint when button is disabled")
	}
	if !strings.Contains(html, `Add at least one recipe to save your list.`) {
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
	applySavedToRecipes(listWithSavedRecipe.Recipes, p)
	w := httptest.NewRecorder()
	FormatShoppingListHTMLForHash(t.Context(), p, listWithSavedRecipe, nil, true, p.Hash(), nil, w)
	html := assertHTTPSuccess(t, w)

	if !strings.Contains(html, `hx-post="/recipes/`) || !strings.Contains(html, `/finalize"`) {
		t.Error("HTML should submit finalize with HTMX POST when a recipe is saved")
	}
	if !strings.Contains(html, `/dismiss"`) {
		t.Error("added recipe should show a hide action")
	}
	if strings.Contains(html, `/save"`) {
		t.Error("added recipe should not show an active add action")
	}
	if !strings.Contains(html, `✓ Added`) {
		t.Error("added recipe should show added state")
	}
	if !strings.Contains(html, `aria-label="Recipe added"`) {
		t.Error("added recipe should show added state as status text")
	}
	if !strings.Contains(html, `aria-label="Recipe added"
        class="inline-flex items-center text-sm font-semibold text-action-green-700"`) {
		t.Error("added recipe should show completed state as plain colored text")
	}
	if strings.Contains(html, `aria-label="Recipe added"
        class="inline-flex items-center justify-center rounded-lg border`) {
		t.Error("added recipe should show completed state as text, not a boxed chip")
	}
	if strings.Contains(html, `aria-disabled="true"`) {
		t.Error("added recipe should not show completed state as a disabled button")
	}
	if strings.Contains(html, `<details class="space-y-4" open`) {
		t.Error("saved recipe details should start collapsed")
	}
	if !strings.Contains(html, `<details open>`) {
		t.Error("shopping list should start expanded when a recipe is saved")
	}
	if strings.Contains(html, `id="finalize-help"`) {
		t.Error("HTML should not render finalize helper text when button is enabled")
	}
	finalizeIndex := strings.Index(html, `id="shopping-finalize-controls"`)
	shoppingListIndex := strings.Index(html, `Shopping list`)
	if finalizeIndex == -1 || shoppingListIndex == -1 || finalizeIndex > shoppingListIndex {
		t.Error("build shopping list controls should render above the shopping list section")
	}
}

func TestFormatShoppingListHTML_ShowsRestoreOnlyWhenRecipeHidden(t *testing.T) {
	listWithDismissedRecipe := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:        "Weeknight Noodles",
				Description:  "Fast noodles",
				Ingredients:  []ai.Ingredient{{Name: "noodles", Quantity: "1 pound", Price: "2.00"}},
				Instructions: []string{"Step 1"},
				Health:       "Balanced",
				DrinkPairing: "Tea",
			},
		},
	}

	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.Dismissed = []ai.Recipe{listWithDismissedRecipe.Recipes[0]}
	w := httptest.NewRecorder()
	FormatShoppingListHTMLForHash(t.Context(), p, listWithDismissedRecipe, nil, true, p.Hash(), nil, w)
	html := assertHTTPSuccess(t, w)

	if !strings.Contains(html, `/save"`) {
		t.Error("hidden recipe should show a restore action")
	}
	if strings.Contains(html, `/dismiss"`) {
		t.Error("hidden recipe should not show an active hide action")
	}
	if !strings.Contains(html, `Restore`) {
		t.Error("hidden recipe should show restore action")
	}
	if strings.Contains(html, `Hide`) {
		t.Error("hidden recipe should not show hide action")
	}
	if strings.Contains(html, `Dismissed`) {
		t.Error("hidden recipe should not show dismissed status text")
	}
	if strings.Contains(html, `✓ Added`) {
		t.Error("hidden recipe should not show added status text")
	}
	if strings.Contains(html, `Details`) {
		t.Error("hidden recipe should not show details action")
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
	FormatShoppingListHTMLForHash(t.Context(), p, list, nil, false, p.Hash(), nil, w)
	html := assertHTTPSuccess(t, w)

	if strings.Contains(html, `type="radio"`) {
		t.Error("HTML should not contain save/dismiss radio inputs when signed out")
	}
	if strings.Contains(html, `Try again, chef`) {
		t.Error("HTML should not contain regenerate action text when signed out")
	}
	if strings.Contains(html, `Save list`) {
		t.Error("HTML should not contain finalize action text when signed out")
	}
	if strings.Contains(html, `Add`) {
		t.Error("HTML should not contain add action text when signed out")
	}
	if strings.Contains(html, `Hide`) {
		t.Error("HTML should not contain hide action text when signed out")
	}
}
