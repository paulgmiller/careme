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

func TestFormatChatHTML_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatChatHTML(p, list, w)
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
	FormatChatHTML(p, list, w)
	html := w.Body.String()

	isValidHTML(t, html)
	if !strings.Contains(html, "quail") {
		t.Error("HTML should contain 'quail'")
	}
}

func TestFormatChatHTML_IncludesClarityScript(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	templates.SetClarity("test456")
	w := httptest.NewRecorder()
	FormatChatHTML(p, list, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatChatHTML_NoClarityWhenEmpty(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	templates.SetClarity("")
	w := httptest.NewRecorder()
	FormatChatHTML(p, list, w)
	if bytes.Contains(w.Body.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatChatHTML_HomePageLink(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatChatHTML(p, list, w)
	html := w.Body.String()

	// Verify "Careme Recipes" is a link to home page
	if !strings.Contains(html, `<a href="/"`) {
		t.Error("HTML should contain a link to home page")
	}
	if !strings.Contains(html, "Careme Recipes</a>") {
		t.Error("HTML should contain 'Careme Recipes' as a link")
	}
}

func TestFormatChatHTML_CollapsibleDetails(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatChatHTML(p, list, w)
	html := w.Body.String()

	// Verify details element exists for collapsible content
	if !strings.Contains(html, "<details") {
		t.Error("HTML should contain <details> element for collapsible content")
	}
	if !strings.Contains(html, "<summary") {
		t.Error("HTML should contain <summary> element for toggle button")
	}
	if !strings.Contains(html, "Details</span>") {
		t.Error("HTML should contain 'Details' text in summary")
	}
}
