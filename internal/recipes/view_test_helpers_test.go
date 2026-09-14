package recipes

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

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

func assertHTTPSuccess(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	return w.Body.String()
}

func formatShoppingListHTMLForTest(ctx context.Context, p *generatorParams, l ai.ShoppingList, signedIn bool, selection recipeSelection, w *httptest.ResponseRecorder) {
	writeShoppingListPage(ctx, w, shoppingListViewInput{
		params:              p,
		list:                l,
		wineRecommendations: nil,
		recipeImages:        nil,
		currentUser:         renderTestUser(signedIn),
		hash:                p.Hash(),
		selection:           selection,
		helpMessage:         "",
		pendingInstructions: "",
		progress:            shoppingProgress{},
	})
}

func renderTestUser(signedIn bool) *utypes.User {
	if !signedIn {
		return nil
	}
	return &utypes.User{
		ID:    "test-user",
		Email: []string{"chef@example.com"},
	}
}

func TestMain(m *testing.M) {
	if err := templates.Init(&config.Config{}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

var list = ai.ShoppingList{
	Recipes: []ai.Recipe{
		{
			Title:       "Test Recipe",
			Description: "A simple quail recipe",
			Properties: ai.RecipeProperties{
				TotalMinutes:         35,
				Servings:             4,
				EstimatedCostDollars: 21,
				CaloriesPerServing:   540,
				CookingMethods:       []ai.CookingMethod{ai.CookingMethodStovetop, ai.CookingMethodOven, ai.CookingMethodOther},
			},
			Ingredients: []ai.Ingredient{
				{Name: "quail", Quantity: "1 cup", Price: "2.00"},
				{Name: "kohlrabi", Quantity: "2 tbsp", Price: "1.50"},
			},
			Instructions: []string{
				"Step 1: Do something.",
				"Step 2: Do something else.",
			},
			Health:       "Brown rice adds fiber but takes longer to cook.",
			DrinkPairing: "Water",
		},
	},
}

func assertInstructionMarkdown(t *testing.T, html string) {
	t.Helper()
	for _, want := range []string{
		"<p>Prepare:</p>",
		"<ul>",
		"<li>1 green bell pepper, diced</li>",
		"<li>4 ounces sweet onion, diced</li>",
		"<p>then toss with the pasta.</p>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("recipe instructions should contain %q: %s", want, html)
		}
	}
}
