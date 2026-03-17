package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes/feedback"
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
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
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
	if !strings.Contains(html, "shopping-wine-refresh:") {
		t.Error("shopping list HTML should include wine refresh history handling")
	}
	if !strings.Contains(html, "Shopping list") {
		t.Error("shopping list HTML should render the shopping list section for a single recipe")
	}
	if !strings.Contains(html, `id="finalize-help"`) {
		t.Error("shopping list HTML should include helper text for disabled finalize state")
	}
	if !strings.Contains(html, `disabled`) {
		t.Error("shopping list HTML should disable finalize button when nothing is saved")
	}
}

func TestFormatMail_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
	html := w.Body.String()

	isValidHTML(t, html)
	if !strings.Contains(html, "quail") {
		t.Error("HTML should contain 'quail'")
	}
}

func TestFormatShoppingListHTML_IncludesClarityScript(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	templates.Clarityproject = "test456"
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatShoppingListHTML_IncludesClaritySessionID(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.Clarityproject
	t.Cleanup(func() {
		templates.Clarityproject = prev
	})
	templates.Clarityproject = "test456"

	ctx := logsetup.WithSessionID(t.Context(), "sess-123")

	w := httptest.NewRecorder()
	FormatShoppingListHTMLForHash(ctx, p, list, nil, true, p.Hash(), w)
	if !bytes.Contains(w.Body.Bytes(), []byte(`window.clarity("identify", "sess-123", "sess-123")`)) {
		t.Error("HTML should include Clarity identify call with session id")
	}
}

func TestFormatShoppingListHTML_NoClarityWhenEmpty(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	templates.Clarityproject = ""
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
	if bytes.Contains(w.Body.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatShoppingListHTML_IncludesGoogleTagScript(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.GoogleTagID
	t.Cleanup(func() {
		templates.GoogleTagID = prev
	})
	templates.GoogleTagID = "AW-1234567890"
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.googletagmanager.com/gtag/js?id=AW-1234567890")) {
		t.Error("HTML should contain Google tag script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("gtag('config', 'AW-1234567890');")) {
		t.Error("HTML should contain Google tag ID")
	}
}

func TestFormatShoppingListHTML_NoGoogleTagWhenEmpty(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	prev := templates.GoogleTagID
	t.Cleanup(func() {
		templates.GoogleTagID = prev
	})
	templates.GoogleTagID = ""
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
	if bytes.Contains(w.Body.Bytes(), []byte("googletagmanager.com")) {
		t.Error("HTML should not contain Google tag script when tag ID is empty")
	}
}

func TestFormatShoppingListHTML_HomePageLink(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	FormatShoppingListHTML(t.Context(), p, list, true, w)
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
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(t.Context(), p, list.Recipes[0], true, []RecipeThreadEntry{}, feedback.Feedback{}, nil, w)
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
	if !strings.Contains(html, "Choose a wine") {
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
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(t.Context(), p, list.Recipes[0], false, []RecipeThreadEntry{}, feedback.Feedback{}, nil, w)
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
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.ConversationID = "convo123"
	w := httptest.NewRecorder()
	FormatRecipeHTML(t.Context(), p, list.Recipes[0], true, []RecipeThreadEntry{}, feedback.Feedback{}, &ai.WineSelection{
		Wines: []ai.Ingredient{
			{Name: "Oregon Pinot Noir", Price: "$14.99"},
			{Name: "Backup Chardonnay", Price: "$11.99"},
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
	if got := strings.Count(html, "Oregon Pinot Noir"); got < 2 {
		t.Errorf("recipe HTML should include wine in ingredients and recommendation, got count %d", got)
	}
	if got := strings.Count(html, "Backup Chardonnay"); got != 1 {
		t.Errorf("recipe HTML should only show backup wine in recommendation, got count %d", got)
	}
	if strings.Contains(html, "choose a wine") {
		t.Error("recipe HTML should not render choose a wine button when recommendation exists")
	}
}

func TestFormatShoppingListHTMLForHash_RendersWinePickerAndWineIngredients(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	multi := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:        "Roast Chicken",
				Description:  "Simple roast",
				Ingredients:  []ai.Ingredient{{Name: "Chicken", Quantity: "1", Price: "$10"}},
				Instructions: []string{"Roast"},
				Health:       "Protein",
				DrinkPairing: "Pinot noir",
			},
			{
				Title:        "Pasta",
				Description:  "Quick pasta",
				Ingredients:  []ai.Ingredient{{Name: "Pasta", Quantity: "1 box", Price: "$2"}},
				Instructions: []string{"Boil"},
				Health:       "Carb-rich",
				DrinkPairing: "Sparkling water",
			},
		},
	}
	wineHash := multi.Recipes[0].ComputeHash()
	pickerHash := multi.Recipes[1].ComputeHash()
	pickerActionID, pickerButtonID := shoppingWineDOMIDs(pickerHash)
	pickerPreviewID := shoppingWinePreviewDOMID(pickerHash)
	pickerDetailID, pickerDetailButtonID := shoppingWineDetailDOMIDs(pickerHash)
	w := httptest.NewRecorder()
	FormatShoppingListHTMLForHash(t.Context(), p, multi, map[string]*ai.WineSelection{
		wineHash: {
			Wines: []ai.Ingredient{
				{Name: "Cellar Red", Quantity: "1 bottle", Price: "$15"},
				{Name: "Second Bottle", Quantity: "1 bottle", Price: "$18"},
			},
			Commentary: "Good with roasted flavors.",
		},
	}, true, p.Hash(), w)
	html := w.Body.String()

	isValidHTML(t, html)

	if !strings.Contains(html, `id="`+pickerActionID+`"`) {
		t.Fatalf("shopping list should include action wine container for recipe without selection, body: %s", html)
	}
	if !strings.Contains(html, `id="`+pickerButtonID+`"`) {
		t.Fatalf("shopping list should include compact wine picker for recipe without selection, body: %s", html)
	}
	if !strings.Contains(html, `id="`+pickerPreviewID+`"`) {
		t.Fatalf("shopping list should include preview wine container for recipe without selection, body: %s", html)
	}
	if !strings.Contains(html, `id="`+pickerDetailID+`"`) {
		t.Fatalf("shopping list should include details wine container for recipe without selection, body: %s", html)
	}
	if !strings.Contains(html, `id="`+pickerDetailButtonID+`"`) {
		t.Fatalf("shopping list should include details wine picker for recipe without selection, body: %s", html)
	}
	if _, wineButtonID := shoppingWineDOMIDs(wineHash); strings.Contains(html, `id="`+wineButtonID+`"`) {
		t.Fatalf("shopping list should not include picker for recipe with cached wine, body: %s", html)
	}
	if !strings.Contains(html, `aria-label="Choose wine"`) {
		t.Fatalf("shopping list should include accessible wine picker label, body: %s", html)
	}
	if strings.Index(html, `aria-live="polite"`) > strings.Index(html, `id="`+pickerPreviewID+`"`) {
		t.Fatalf("shopping list should render wine preview beneath the action row, body: %s", html)
	}
	if got := strings.Count(html, "Cellar Red"); got != 4 {
		t.Fatalf("shopping list should show selected wine in ingredients, preview, recommendation, and combined list; got count %d, body: %s", got, html)
	}
	if got := strings.Count(html, "Second Bottle"); got != 2 {
		t.Fatalf("shopping list should only add the second wine to preview and recommendation; got count %d, body: %s", got, html)
	}
	if got := strings.Count(html, "Good with roasted flavors."); got != 1 {
		t.Fatalf("shopping list should render wine commentary once in details; got count %d, body: %s", got, html)
	}
	if strings.Index(html, "Drink pairing:") > strings.Index(html, "Good with roasted flavors.") {
		t.Fatalf("shopping list should render wine commentary beneath drink pairing, body: %s", html)
	}
}

func TestFormatShoppingRecipeWineHTML_RendersPicker(t *testing.T) {
	w := httptest.NewRecorder()
	FormatShoppingRecipeWineHTML("recipe-hash", "action", nil, w)
	body := w.Body.String()
	actionID, _ := shoppingWineDOMIDs("recipe-hash")
	previewID := shoppingWinePreviewDOMID("recipe-hash")
	detailContainerID, _ := shoppingWineDetailDOMIDs("recipe-hash")

	if !strings.Contains(body, `id="`+actionID+`"`) {
		t.Fatalf("expected shopping wine fragment container in response, got body: %s", body)
	}
	if !strings.Contains(body, `id="`+previewID+`"`) || !strings.Contains(body, `id="`+detailContainerID+`"`) || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected shopping wine preview and details fragments to update out-of-band, got body: %s", body)
	}
	if !strings.Contains(body, `aria-label="Choose wine"`) {
		t.Fatalf("expected accessible wine picker in response, got body: %s", body)
	}
	if !strings.Contains(body, `hx-post="/recipe/recipe-hash/wine?view=shopping&slot=action"`) {
		t.Fatalf("expected shopping wine endpoint in response, got body: %s", body)
	}
	if !strings.Contains(body, `sessionStorage.setItem('shopping-wine-refresh:`) {
		t.Fatalf("expected shopping wine picker to mark the page for refresh after browser back, got body: %s", body)
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
