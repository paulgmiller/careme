package recipes

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/static"
	"github.com/stretchr/testify/assert"
)

func TestRecipeViewsRenderInstructionMarkdownListWithinProse(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	recipe := ai.Recipe{
		Title:       "Pepper Pasta",
		Description: "A quick pasta dinner.",
		Instructions: []string{
			"Prepare:\n\n- 1 green bell pepper, diced\n- 4 ounces sweet onion, diced\n\nthen toss with the pasta.",
		},
	}

	t.Run("single recipe", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeRecipePage(t.Context(), w, recipeViewInput{
			params:             p,
			recipe:             recipe,
			saved:              false,
			currentUser:        renderTestUser(true),
			recipeCritique:     nil,
			hasRecipeImage:     false,
			thread:             nil,
			feedback:           feedback.Feedback{},
			wineRecommendation: nil,
		})
		html := assertHTTPSuccess(t, w)
		isValidHTML(t, html)
		assertInstructionMarkdown(t, html)
	})

	t.Run("shopping list", func(t *testing.T) {
		w := httptest.NewRecorder()
		formatShoppingListHTMLForTest(t.Context(), p, ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, true, recipeSelection{}, w)
		html := assertHTTPSuccess(t, w)
		isValidHTML(t, html)
		assertInstructionMarkdown(t, html)
	})
}

func TestFormatRecipeHTML_NoFinalizeOrRegenerate(t *testing.T) {
	lat := 47.6097
	lon := -122.3331
	loc := locations.Location{
		ID: "70000001", Name: "Store", Address: "1 Main St", ZipCode: "98101",
		Lat: &lat, Lon: &lon,
	}
	p := DefaultParams(&loc, time.Date(2026, time.January, 25, 0, 0, 0, 0, time.UTC))
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	recipe.OriginHash = p.Hash()
	w := httptest.NewRecorder()
	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        renderTestUser(true),
		recipeCritique:     nil,
		hasRecipeImage:     false,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)

	if !strings.Contains(html, `<meta name="description" content="A simple quail recipe Recipe for Store on 2026-01-25." />`) {
		t.Error("recipe HTML should include recipe, location, and date in the meta description")
	}
	assert.Contains(t, html, `<a href="/locations?lat=47.6097&amp;lon=-122.3331"`)
	assert.NotContains(t, html, `/locations?zip=`)
	assert.Contains(t, html, `>Store</a>`)
	assert.NotContains(t, html, `<form method="POST" action="/recipes" class="inline">`)
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
	if !strings.Contains(html, `/static/htmx@2.0.10.js`) {
		t.Error("recipe HTML should include htmx script")
	}
	if !strings.Contains(html, `aria-label="Share recipe"`) {
		t.Error("recipe HTML should include a share button")
	}
	if !strings.Contains(html, `data-share-status`) {
		t.Error("recipe share button should include visible copy feedback")
	}
	if !strings.Contains(html, `data-share-url="/recipe/`) {
		t.Error("recipe share button should share the stable recipe URL")
	}
	if !strings.Contains(html, `id="question-thread"`) {
		t.Error("recipe HTML should contain question thread container")
	}
	if !strings.Contains(html, `id="wine-recommendation"`) {
		t.Error("recipe HTML should contain wine recommendation container")
	}
	if strings.Contains(html, `/wine"`) || strings.Contains(html, "Choose a wine") {
		t.Error("recipe HTML should not include manual wine picker")
	}
	if !strings.Contains(html, `/save"`) || !strings.Contains(html, `Save`) {
		t.Error("recipe HTML should include save button")
	}
	if strings.Contains(html, "See plated dish") || strings.Contains(html, "Sign in to see plated dish") {
		t.Error("recipe HTML should not include manual image generation actions")
	}
	for _, want := range []string{"Total time:", "35 min", "Servings:", "4 servings", "Estimated total cost:", "$21", "Calories per serving:", "540 cal", "Stovetop", "Oven", "Other", "Health:", "Brown rice adds fiber"} {
		assert.Contains(t, html, want)
	}
	assert.NotContains(t, html, "Health note:")
	assert.NotContains(t, html, "🌿")
	propertyIndex := strings.Index(html, `aria-label="Recipe details"`)
	ingredientsIndex := strings.Index(html, `id="recipe-ingredients"`)
	assert.NotEqual(t, -1, propertyIndex)
	assert.NotEqual(t, -1, ingredientsIndex)
	assert.Less(t, propertyIndex, ingredientsIndex, "recipe properties should appear before ingredients")
	healthIndex := strings.Index(html, "Health:</span>")
	assert.Greater(t, healthIndex, ingredientsIndex, "health should render with the free-form recipe details")
	if !strings.Contains(html, `sm:grid-cols-[minmax(0,1fr)_10rem_5rem]`) {
		t.Error("recipe HTML should render ingredient rows with responsive aligned columns")
	}
	assert.Contains(t, html, `id="recipe-instructions" data-recipe-steps`)
	assert.Equal(t, len(recipe.Instructions), strings.Count(html, `data-recipe-step>`))
	assert.Contains(t, html, `<ol class="recipe-step-list mt-3 space-y-2`)
	assert.Contains(t, html, `aria-label="Mark step 1 done"`)
	assert.Contains(t, html, `aria-label="Mark step 2 done"`)
	assert.Equal(t, len(recipe.Instructions), strings.Count(html, `data-recipe-step-done`))
	assert.Contains(t, html, `data-recipe-step-undo`)
	assert.NotContains(t, html, `data-recipe-step-reset`)
	assert.NotContains(t, html, `Show all steps`)
	assert.NotContains(t, html, `data-recipe-step-status`)
	assert.NotContains(t, html, `data-recipe-step-message`)
	assert.Contains(t, html, `href="/temperature-guide"`)
	assert.Contains(t, html, `>See the temperature guide</a>`)
	assert.Contains(t, html, `Swipe a step aside or click its number when it’s done.`)
	assert.Contains(t, html, `<script src="`+static.AssetPath+`recipe.js"></script>`)
	assert.NotContains(t, html, `initializeRecipeSteps`)
	assert.Regexp(t, `<details id="recipe-ingredients"[^>]*class="recipe-ingredients group"[^>]*\sopen>`, html)
	if strings.Contains(html, `flex flex-wrap items-center justify-between gap-2 rounded-lg bg-brand-50 px-3 py-2 text-sm`) {
		t.Error("recipe HTML should no longer use the old wrapped ingredient row layout")
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
	if strings.Contains(html, "Recipe score:") {
		t.Error("recipe HTML should hide recipe score when no critique exists")
	}
	if !strings.Contains(html, "chef@example.com") {
		t.Error("recipe HTML should render signed-in account widget")
	}
	if !strings.Contains(html, `href="/admin/prompt/recipe/`+recipe.ComputeHash()+`"`) {
		t.Error("recipe HTML should link to admin recipe prompt")
	}
	if !strings.Contains(html, `>Admin</a>`) {
		t.Error("recipe HTML should label the prompt link Admin")
	}
	if strings.Contains(html, `href="/admin/mealplan/`) {
		t.Error("recipe HTML should not link to the origin shopping list admin page")
	}
}

func TestFormatRecipeHTML_HidesQuestionInputWhenSignedOut(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	w := httptest.NewRecorder()
	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        nil,
		recipeCritique:     nil,
		hasRecipeImage:     false,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)

	if strings.Contains(html, `name="question"`) {
		t.Error("recipe HTML should not contain question input when signed out")
	}
	if strings.Contains(html, `/save"`) {
		t.Error("recipe HTML should not expose save action when signed out")
	}
	if !strings.Contains(html, "Sign in to ask follow-up questions") {
		t.Error("recipe HTML should prompt signed-out users to sign in for questions")
	}
	if strings.Contains(html, `/recipe/`) && strings.Contains(html, `/wine"`) {
		t.Error("recipe HTML should not expose wine picker htmx endpoint when signed out")
	}
	if strings.Contains(html, `name="feedback"`) {
		t.Error("recipe HTML should not contain feedback form when signed out")
	}
	if !strings.Contains(html, `href="/sign-in?return_to_b64=`) {
		t.Error("recipe HTML should render a header sign-in link when signed out")
	}
}

func TestFormatRecipeHTML_ShowsRecipeCritiqueScore(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	w := httptest.NewRecorder()
	score := 7
	recipeCritique := &ai.RecipeCritique{OverallScore: score, Model: "anthropic/claude-opus-5"}

	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        renderTestUser(true),
		recipeCritique:     recipeCritique,
		hasRecipeImage:     false,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	assert.Contains(t, html, "Recipe score:", "recipe HTML should contain recipe score text")
	assert.Contains(t, html, `href="/critiques/`, "recipe HTML should contain public critique link")
	assert.Contains(t, html, ">7/10<", "recipe HTML should contain critique score value")
	assert.NotContains(t, html, "This recipe may need another look before cooking.", "recipe HTML should not show low-score warning at the retry threshold")
}

func TestFormatRecipeHTML_ShowsProminentWarningForLowCritiqueScore(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	w := httptest.NewRecorder()
	score := 6
	recipeCritique := &ai.RecipeCritique{OverallScore: score, Model: "anthropic/claude-opus-5"}

	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        renderTestUser(true),
		recipeCritique:     recipeCritique,
		hasRecipeImage:     false,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	assert.Contains(t, html, "This recipe may need another look before cooking.", "recipe HTML should show a prominent low-score warning")
	assert.Contains(t, html, "It scored 6/10, below our 7/10 retry mark.", "recipe HTML should explain why the warning appears")
	assert.Contains(t, html, `Read the critique`, "recipe warning should contain critique link text")
	assert.Contains(t, html, `href="/critiques/`, "recipe warning should link to the public critique")
}

func TestFormatRecipeHTML_RendersCachedWineRecommendation(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	w := httptest.NewRecorder()
	writeRecipePage(t.Context(), w, recipeViewInput{
		params:         p,
		recipe:         recipe,
		saved:          false,
		currentUser:    renderTestUser(true),
		recipeCritique: nil,
		hasRecipeImage: false,
		thread:         []RecipeThreadEntry{},
		feedback:       feedback.Feedback{},
		wineRecommendation: &ai.WineSelection{
			Wines: []ai.Ingredient{
				{Name: "Oregon Pinot Noir", Price: "$14.99"},
				{Name: "Backup Chardonnay", Price: "$11.99"},
			},
			Commentary: "Great with the savory notes.",
		},
	})
	html := assertHTTPSuccess(t, w)

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
	if strings.Contains(html, "Choose a wine") {
		t.Error("recipe HTML should not render the wine picker when recommendation exists")
	}
}

func TestFormatRecipeHTML_AllowsIngredientWithoutPrice(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	recipe := ai.Recipe{
		Title:        "Market Greens",
		Description:  "Simple salad",
		CookTime:     "10 minutes",
		CostEstimate: "$8-10",
		Ingredients: []ai.Ingredient{
			{Name: "Little gem lettuce", Quantity: "2 heads", Price: ""},
		},
		Instructions: []string{"Wash and plate."},
		Health:       "",
		DrinkPairing: "Sparkling water",
		ResponseID:   "resp-123",
	}

	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        renderTestUser(true),
		recipeCritique:     nil,
		hasRecipeImage:     false,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	if !strings.Contains(html, "Little gem lettuce") {
		t.Fatal("recipe HTML should include ingredient name when price is empty")
	}
	if !strings.Contains(html, "2 heads") {
		t.Fatal("recipe HTML should include ingredient quantity when price is empty")
	}
	if !strings.Contains(html, `hidden sm:block`) {
		t.Fatal("recipe HTML should reserve desktop alignment when ingredient price is empty")
	}
	assert.NotContains(t, html, "Health:</span>")
}

func TestFormatRecipeHTML_RendersRecipeImage(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	recipe := list.Recipes[0]
	recipe.ResponseID = "resp-123"
	recipeHash := recipe.ComputeHash()

	writeRecipePage(t.Context(), w, recipeViewInput{
		params:             p,
		recipe:             recipe,
		saved:              false,
		currentUser:        renderTestUser(true),
		recipeCritique:     nil,
		hasRecipeImage:     true,
		thread:             []RecipeThreadEntry{},
		feedback:           feedback.Feedback{},
		wineRecommendation: nil,
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)

	if !strings.Contains(html, `id="recipe-image-panel"`) {
		t.Fatal("recipe HTML should render the recipe image panel")
	}
	if !strings.Contains(html, "/recipe/"+recipeHash+"/image") {
		t.Fatalf("recipe HTML should render the cached recipe image URL, got body: %s", html)
	}
	if strings.Contains(html, "View dish image") || strings.Contains(html, "See plated dish") {
		t.Fatalf("recipe HTML should not render an image action when an image exists, got body: %s", html)
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

	writeRecipeThread(w, newRecipeThreadView(thread, true, ai.ResponseRef{
		ID:             "conv123",
		PromptCacheKey: "careme:store-day:v1:test",
	}, "recipe123"))
	body := assertHTTPSuccess(t, w)

	newerIndex := strings.Index(body, "newer question")
	olderIndex := strings.Index(body, "older question")
	if newerIndex == -1 || olderIndex == -1 {
		t.Fatalf("expected both questions in output, body: %s", body)
	}
	if newerIndex > olderIndex {
		t.Fatalf("expected newer question before older question, body: %s", body)
	}
	if !strings.Contains(body, "<details") {
		t.Fatalf("expected thread entries to render as expandable details, body: %s", body)
	}
	if !strings.Contains(body, "<details class=\"group rounded-xl border border-brand-100 bg-brand-50/70 p-4\" open>") {
		t.Fatalf("expected newest thread entry to be expanded by default, body: %s", body)
	}
	if !strings.Contains(body, "group-open:hidden") || !strings.Contains(body, "group-open:block") {
		t.Fatalf("expected thread entries to include chevron expand/collapse indicator, body: %s", body)
	}
	if !strings.Contains(body, `name="prompt_cache_key" value="careme:store-day:v1:test"`) {
		t.Fatalf("expected thread fragment to preserve prompt cache key, body: %s", body)
	}
}

func TestFormatRecipeThreadHTML_RendersEmptyContinuationFields(t *testing.T) {
	w := httptest.NewRecorder()
	writeRecipeThread(w, newRecipeThreadView(nil, true, ai.ResponseRef{}, "recipe123"))
	body := assertHTTPSuccess(t, w)

	assert.Contains(t, body, `name="response_id" value=""`)
	assert.Contains(t, body, `name="prompt_cache_key" value=""`)
}
