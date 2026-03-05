package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/templates"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestMain(m *testing.M) {
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

var list = ai.ShoppingList{
	Recipes: []ai.Recipe{
		{
			Title:        "Test Recipe",
			Description:  "A simple quail recipe",
			CookTime:     "35 minutes",
			CostEstimate: "$18-24",
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
	if !strings.Contains(html, "Cook time:") {
		t.Error("shopping list HTML should contain cook time")
	}
	if !strings.Contains(html, "Estimated cost:") {
		t.Error("shopping list HTML should contain estimated cost")
	}
	if !strings.Contains(html, `/static/htmx@2.0.8.js`) {
		t.Error("shopping list HTML should include htmx script")
	}
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

	templates.Clarityproject = "test456"
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
	templates.Clarityproject = ""
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	if bytes.Contains(w.Body.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatShoppingListHTML_IncludesGoogleTagScript(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.GoogleTagID
	t.Cleanup(func() {
		templates.GoogleTagID = prev
	})
	templates.GoogleTagID = "AW-1234567890"
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.googletagmanager.com/gtag/js?id=AW-1234567890")) {
		t.Error("HTML should contain Google tag script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("gtag('config', 'AW-1234567890');")) {
		t.Error("HTML should contain Google tag ID")
	}
}

func TestFormatShoppingListHTML_NoGoogleTagWhenEmpty(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	prev := templates.GoogleTagID
	t.Cleanup(func() {
		templates.GoogleTagID = prev
	})
	templates.GoogleTagID = ""
	w := httptest.NewRecorder()
	FormatShoppingListHTML(p, list, true, w)
	if bytes.Contains(w.Body.Bytes(), []byte("googletagmanager.com")) {
		t.Error("HTML should not contain Google tag script when tag ID is empty")
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
	FormatRecipeHTML(p, list.Recipes[0], true, []RecipeThreadEntry{}, RecipeFeedback{}, nil, w)
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
	if !strings.Contains(html, `name="recipe_title"`) {
		t.Error("recipe HTML should include recipe title hidden input")
	}
	if !strings.Contains(html, `/static/htmx@2.0.8.js`) {
		t.Error("recipe HTML should include htmx script")
	}
	if !strings.Contains(html, `id="question-thread"`) {
		t.Error("recipe HTML should contain question thread container")
	}
	if !strings.Contains(html, `id="wine-recommendation"`) {
		t.Error("recipe HTML should contain wine recommendation container")
	}
	if !strings.Contains(html, `hx-post="/recipe/`) || !strings.Contains(html, `/wine"`) {
		t.Error("recipe HTML should include wine picker htmx endpoint")
	}
	if !strings.Contains(html, "choose a wine") {
		t.Error("recipe HTML should include choose a wine button")
	}
	if !strings.Contains(html, "Cook time:") {
		t.Error("recipe HTML should contain cook time")
	}
	if !strings.Contains(html, "Estimated cost:") {
		t.Error("recipe HTML should contain estimated cost")
	}
	if !strings.Contains(html, `id="question-error"`) {
		t.Error("recipe HTML should contain question error surface")
	}
	if !strings.Contains(html, `hx-on::response-error=`) {
		t.Error("recipe HTML should define htmx response-error behavior")
	}
	if !strings.Contains(html, "I cooked it!") {
		t.Error("recipe HTML should contain I cooked it button")
	}
	if !strings.Contains(html, `name="stars"`) {
		t.Error("recipe HTML should contain stars feedback controls")
	}
	if !strings.Contains(html, `name="feedback"`) {
		t.Error("recipe HTML should contain text feedback control")
	}
}

func TestFormatRecipeHTML_HidesQuestionInputWhenSignedOut(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(p, list.Recipes[0], false, []RecipeThreadEntry{}, RecipeFeedback{}, nil, w)
	html := w.Body.String()

	isValidHTML(t, html)

	if strings.Contains(html, `name="question"`) {
		t.Error("recipe HTML should not contain question input when signed out")
	}
	if !strings.Contains(html, "Sign in to ask follow-up questions.") {
		t.Error("recipe HTML should prompt signed-out users to sign in for questions")
	}
}

func TestFormatRecipeHTML_RendersCachedWineRecommendation(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(p, list.Recipes[0], true, []RecipeThreadEntry{}, RecipeFeedback{}, &ai.WineSelection{
		Wines: []ai.Ingredient{
			{Name: "Oregon Pinot Noir", Price: "$14.99"},
		},
		Commentary: "Great with the savory notes.",
	}, w)
	html := w.Body.String()

	isValidHTML(t, html)

	if !strings.Contains(html, "Oregon Pinot Noir") || !strings.Contains(html, "$14.99") {
		t.Error("recipe HTML should render cached wine picks with prices")
	}
	if !strings.Contains(html, "Great with the savory notes.") {
		t.Error("recipe HTML should render cached wine commentary")
	}
	if strings.Contains(html, "choose a wine") {
		t.Error("recipe HTML should not render choose a wine button when recommendation exists")
	}
}

func TestFormatRecipeThreadHTML_SortsNewestFirst(t *testing.T) {
	w := httptest.NewRecorder()
	now := time.Now()
	thread := []RecipeThreadEntry{
		{
			Question:  "older question",
			Answer:    "older answer",
			CreatedAt: now.Add(-1 * time.Hour),
		},
		{
			Question:  "newer question",
			Answer:    "newer answer",
			CreatedAt: now,
		},
	}

	FormatRecipeThreadHTML(thread, true, "conv123", w)
	body := w.Body.String()

	newerIndex := strings.Index(body, "newer question")
	olderIndex := strings.Index(body, "older question")
	if newerIndex == -1 || olderIndex == -1 {
		t.Fatalf("expected both questions in output, body: %s", body)
	}
	if newerIndex > olderIndex {
		t.Fatalf("expected newer question before older question, body: %s", body)
	}
}

func TestFormatRecipeWineHTML_RendersRecommendation(t *testing.T) {
	w := httptest.NewRecorder()

	FormatRecipeWineHTML("recipe-hash", &ai.WineSelection{
		Wines: []ai.Ingredient{
			{Name: "Light Pinot Noir", Price: "$12.99"},
			{Name: "Dry Rose", Price: "$10.50"},
		},
		Commentary: "Try a light pinot noir.",
	}, w)
	body := w.Body.String()

	if !strings.Contains(body, `id="wine-recommendation"`) {
		t.Fatalf("expected wine fragment container in response, got body: %s", body)
	}
	if !strings.Contains(body, "Light Pinot Noir") || !strings.Contains(body, "$12.99") {
		t.Fatalf("expected wine picks with prices in response, got body: %s", body)
	}
	if !strings.Contains(body, "Try a light pinot noir.") {
		t.Fatalf("expected recommendation in response, got body: %s", body)
	}
	if strings.Index(body, "Light Pinot Noir") > strings.Index(body, "Try a light pinot noir.") {
		t.Fatalf("expected wine picks to render before commentary, got body: %s", body)
	}
}
