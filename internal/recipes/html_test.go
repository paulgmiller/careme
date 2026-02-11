package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/templates"
	"net/http"
	"net/http/httptest"
	"strings"
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
			Description: "A simple quail recipe",
			Ingredients: []ai.Ingredient{
				{Name: "quail", Quantity: "1 cup", Price: "2.00"},
				{Name: "kohlrabi", Quantity: "2 tbsp", Price: "1.50"},
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

func TestFormatShoppingListHTML_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	html := w.Body.String()
	if w.Code != http.StatusOK {
		t.Error("Want ok statuscode")
	}
	isValidHTML(t, html)
}

func TestFormatMail_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	html := w.Body.String()

	isValidHTML(t, html)
	if !strings.Contains(html, "quail") {
		t.Error("HTML should contain 'quail'")
	}
}

func TestFormatShoppingListHTML_IncludesClarityScript(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	templates.SetClarity("test456")
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatShoppingListHTML_NoClarityWhenEmpty(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	templates.SetClarity("")
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	if bytes.Contains(w.Body.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatShoppingListHTML_HomePageLink(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	html := w.Body.String()

	// Verify "Careme Recipes" is a link to home page
	if !strings.Contains(html, `<a href="/"`) {
		t.Error("HTML should contain a link to home page")
	}
	if !strings.Contains(html, "Careme Recipes</a>") {
		t.Error("HTML should contain 'Careme Recipes' as a link")
	}
}

func TestFormatRecipeHTML_NoFinalizeOrRegenerate(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(p, list.Recipes[0], true, []RecipeThreadEntry{}, w)
	html := w.Body.String()

	isValidHTML(t, html)

	if strings.Contains(html, "Finalize") {
		t.Error("recipe HTML should not contain Finalize button")
	}
	if strings.Contains(html, "Regenerate") {
		t.Error("recipe HTML should not contain Regenerate button")
	}
	if strings.Contains(html, `name="saved"`) || strings.Contains(html, `name="dismissed"`) {
		t.Error("recipe HTML should not contain save/dismiss inputs")
	}
	if !strings.Contains(html, `name="question"`) {
		t.Error("recipe HTML should contain question input")
	}
	if !strings.Contains(html, `/static/htmx@2.0.8.js`) {
		t.Error("recipe HTML should include htmx script")
	}
	if !strings.Contains(html, `id="question-thread"`) {
		t.Error("recipe HTML should contain question thread container")
	}
	if !strings.Contains(html, `id="question-loading"`) {
		t.Error("recipe HTML should contain question loading indicator")
	}
}

func TestFormatRecipeHTML_HidesQuestionInputWhenSignedOut(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(p, list.Recipes[0], false, []RecipeThreadEntry{}, w)
	html := w.Body.String()

	isValidHTML(t, html)

	if strings.Contains(html, `name="question"`) {
		t.Error("recipe HTML should not contain question input when signed out")
	}
	if !strings.Contains(html, "Sign in to ask follow-up questions.") {
		t.Error("recipe HTML should prompt signed-out users to sign in for questions")
	}
}
