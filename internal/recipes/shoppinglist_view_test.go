package recipes

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
)

func TestFormatShoppingListHTML_ValidHTML(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	html := assertHTTPSuccess(t, w)
	isValidHTML(t, html)
	for _, want := range []string{"⏱️", "Total time:", "35 min", "👥", "Servings:", "4 servings", "💵", "Estimated total cost:", "$21", "❤️", "Calories per serving:", "540 cal", "🍳", "Stovetop", "♨️", "Oven", "❓", "Other", "Health:", "Brown rice adds fiber"} {
		assert.Contains(t, html, want)
	}
	assert.NotContains(t, html, "Health note:")
	assert.NotContains(t, html, "🌿")
	if !strings.Contains(html, `/static/htmx@2.0.10.js`) {
		t.Error("shopping list HTML should include htmx script")
	}
	if !strings.Contains(html, `aria-label="Share shopping list"`) {
		t.Error("shopping list HTML should include a share button")
	}
	if !strings.Contains(html, `data-share-status`) {
		t.Error("shopping list share button should include visible copy feedback")
	}
	if !strings.Contains(html, `data-share-url="/recipes?h=`) {
		t.Error("shopping list share button should share the stable shopping list URL")
	}
	if strings.Contains(html, "Shopping list") {
		t.Error("shopping list HTML should not render the shopping list section before a recipe is added")
	}
	if !strings.Contains(html, `sm:grid-cols-[minmax(0,1fr)_10rem_5rem]`) {
		t.Error("shopping list HTML should render ingredient rows with responsive aligned columns")
	}
	if strings.Contains(html, `flex flex-wrap items-center justify-between gap-2 rounded-lg bg-brand-50 px-3 py-2 text-sm`) {
		t.Error("shopping list HTML should no longer use the old wrapped ingredient row layout")
	}
	assert.Contains(t, html, `href="/temperature-guide"`)
	assert.Contains(t, html, `>See the temperature guide</a>`)
	if !strings.Contains(html, `id="finalize-help"`) {
		t.Error("shopping list HTML should include helper text for disabled finalize state")
	}
	if !strings.Contains(html, `disabled`) {
		t.Error("shopping list HTML should disable finalize button when nothing is saved")
	}
	if !strings.Contains(html, "chef@example.com") {
		t.Error("shopping list HTML should render signed-in account widget")
	}
	if !strings.Contains(html, `href="/admin/mealplan/`+p.Hash()+`"`) {
		t.Error("shopping list HTML should link to admin meal plan")
	}
	if !strings.Contains(html, `>Admin</a>`) {
		t.Error("shopping list HTML should label the meal plan link Admin")
	}
	if strings.Contains(html, `href="/admin/prompt/menu/`) {
		t.Error("shopping list HTML should not link directly to the admin menu prompt")
	}
	if strings.Contains(html, `href="/admin/ingredients/`) {
		t.Error("shopping list HTML should not link directly to admin ingredients")
	}
}

func TestFormatShoppingListHTML_ChefNotesUsesPreviousInstructionsAsPlaceholder(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	p.Instructions = "make it vegetarian"
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `name="instructions"`)
	assert.Contains(t, html, `placeholder="make it vegetarian"`)
	assert.NotContains(t, html, `value="make it vegetarian"`)
}

func TestFormatShoppingListHTML_ChefNotesUsesMenuPlanSuggestionWithoutPreviousInstructions(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	menuList := list
	menuList.Plan = &ai.MenuPlan{ChefNoteSuggestion: "make the quail faster."}
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, menuList, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `name="instructions"`)
	assert.Regexp(t, `placeholder="make the quail faster\.?"`, html)
}

func TestFormatShoppingListHTML_ChefNotesUsesEmptyWithoutMenuPlanSuggestions(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `name="instructions"`)
	assert.Regexp(t, `placeholder="e.g. make it vegetarian"`, html)
}

func TestFormatShoppingListHTML_UsesTodaysIngredientsForOldList(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	withNow(t, time.Date(2026, time.January, 15, 18, 0, 0, 0, time.UTC))
	lat := 47.61
	lon := -122.33
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St", ZipCode: "98101", Lat: &lat, Lon: &lon}
	p := DefaultParams(&loc, time.Date(2026, time.January, 12, 0, 0, 0, 0, time.UTC))
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `method="POST"`)
	assert.Contains(t, html, `action="/recipes"`)
	assert.Contains(t, html, `name="location" value="70000001"`)
	assert.Contains(t, html, `Using ingredients from <span class="font-semibold text-brand-700">January 12, 2026</span>.`)
	assert.Contains(t, html, "Use today's ingredients")
	assert.NotContains(t, html, `Chef notes`)
	assert.NotContains(t, html, `name="instructions"`)
	assert.NotContains(t, html, "Try again, chef")
	assert.NotContains(t, html, `Older list`)
	assert.NotContains(t, html, `/regenerate"`)
}

func TestFormatShoppingListHTML_UsesRegenerateForRecentList(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	withNow(t, time.Date(2026, time.January, 15, 18, 0, 0, 0, time.UTC))
	lat := 47.61
	lon := -122.33
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St", ZipCode: "98101", Lat: &lat, Lon: &lon}
	p := DefaultParams(&loc, time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC))
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `method="POST"`)
	assert.Contains(t, html, `/regenerate"`)
	assert.Contains(t, html, "Note to chef")
	assert.Contains(t, html, "Try again, chef")
	assert.NotContains(t, html, "Use today's ingredients")
	assert.NotContains(t, html, `Older list`)
	assert.NotContains(t, html, `Using ingredients from`)
	assert.NotContains(t, html, `January 15, 2026`)
	assert.NotContains(t, html, `name="location" value="70000001"`)
}

func TestFormatShoppingListHTML_ShowsLocationWithoutDateWhenFresh(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Date(2026, time.January, 25, 0, 0, 0, 0, time.UTC))
	w := httptest.NewRecorder()

	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, `Location: <span class="font-semibold text-brand-700">Store</span>`)
	assert.Contains(t, html, `<span class="text-sm text-ink-500">(1 Main St)</span>`)
	assert.NotContains(t, html, `Ingredients from`)
	assert.NotContains(t, html, `Using ingredients from`)
	assert.NotContains(t, html, `January 25, 2026`)
}

func TestFormatShoppingListHTML_ShowsCampaignHelpMessage(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()

	writeShoppingListPage(t.Context(), w, shoppingListViewInput{
		params:              p,
		list:                list,
		wineRecommendations: nil,
		recipeImages:        nil,
		currentUser:         renderTestUser(true),
		hash:                p.Hash(),
		selection:           recipeSelection{},
		helpMessage:         "Save two dinners before building your shopping list.",
		pendingInstructions: "",
		progress:            shoppingProgress{},
	})

	html := assertHTTPSuccess(t, w)
	assert.Contains(t, html, "Welcome to Careme")
	assert.Contains(t, html, "Save two dinners before building your shopping list.")
	assert.Contains(t, html, `aria-label="Dismiss welcome message"`)
	assert.Contains(t, html, `for="shopping-list-help-dismiss"`)
	assert.Contains(t, html, `peer-checked/help:hidden`)
	assert.NotContains(t, html, `<script src="/static/shoppinglist.js"></script>`)
	assert.NotContains(t, html, "localStorage")
}

func TestFormatShoppingListHTML_ShoppingListUsesOnlyAddedRecipes(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	addedRecipe := ai.Recipe{
		Title:        "Added Bowl",
		Description:  "Selected dinner",
		Ingredients:  []ai.Ingredient{{Name: "Added carrots", Quantity: "2 cups"}},
		Instructions: []string{"Cook."},
		Health:       "Balanced",
		DrinkPairing: "Water",
	}
	unaddedRecipe := ai.Recipe{
		Title:        "Maybe Pasta",
		Description:  "Not selected",
		Ingredients:  []ai.Ingredient{{Name: "Unadded noodles", Quantity: "1 box"}},
		Instructions: []string{"Boil."},
		Health:       "Filling",
		DrinkPairing: "Tea",
	}
	shoppingList := ai.ShoppingList{Recipes: []ai.Recipe{addedRecipe, unaddedRecipe}}
	p := DefaultParams(&loc, time.Date(2026, time.January, 25, 0, 0, 0, 0, time.UTC))
	selection := recipeSelection{SavedHashes: []string{addedRecipe.ComputeHash()}}

	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, shoppingList, true, selection, w)
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	if !strings.Contains(html, `<meta name="description" content="Recipes for Store on 2026-01-25: Added Bowl, Maybe Pasta." />`) {
		t.Error("shopping list HTML should include all recipe titles, location, and date in the meta description")
	}
	if !strings.Contains(html, "Shopping list") {
		t.Fatal("shopping list HTML should render the shopping list section when a recipe is added")
	}
	if got := strings.Count(html, "Added carrots"); got != 2 {
		t.Fatalf("added recipe ingredient should render in recipe details and shopping list, got count %d", got)
	}
	if got := strings.Count(html, "Unadded noodles"); got != 1 {
		t.Fatalf("unadded recipe ingredient should render only in recipe details, got count %d", got)
	}
}

func TestFormatShoppingListHTML_GroupsShoppingListByAisle(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	shoppingList := ai.ShoppingList{Recipes: []ai.Recipe{{
		Title:       "Added Dinner",
		Description: "Selected dinner",
		Ingredients: []ai.Ingredient{
			{Name: "Butter", Quantity: "2 tbsp", AisleNumber: "dairy-eggs"},
			{Name: "Milk", Quantity: "1 cup", AisleNumber: "dairy-eggs"},
			{Name: "Beans", Quantity: "1 can", AisleNumber: "2"},
			{Name: "Salt", Quantity: "1 tsp"},
		},
		Instructions: []string{"Cook."},
		Health:       "Balanced",
		DrinkPairing: "Water",
	}}}
	p := DefaultParams(&loc, time.Now())
	selection := recipeSelection{SavedHashes: []string{shoppingList.Recipes[0].ComputeHash()}}

	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, shoppingList, true, selection, w)
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	if got := strings.Count(html, ">Dairy &amp; eggs<"); got != 1 {
		t.Fatalf("shopping list should render dairy aisle heading once, got %d", got)
	}
	if got := strings.Count(html, ">Aisle 2<"); got != 1 {
		t.Fatalf("shopping list should render numeric aisle heading once, got %d", got)
	}
	if got := strings.Count(html, ">Other items<"); got != 1 {
		t.Fatalf("shopping list should render missing aisle heading once, got %d", got)
	}
}

func TestFormatShoppingListHTML_IncludesClarityScript(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.Clarityproject
	t.Cleanup(func() {
		templates.Clarityproject = prev
	})
	templates.Clarityproject = "test456"
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	assertHTTPSuccess(t, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatShoppingListHTML_IncludesClaritySessionID(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.Clarityproject
	t.Cleanup(func() {
		templates.Clarityproject = prev
	})
	templates.Clarityproject = "test456"

	ctx := logsetup.WithSessionID(t.Context(), "sess-123")

	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(ctx, p, list, true, recipeSelection{}, w)
	assertHTTPSuccess(t, w)
	if !bytes.Contains(w.Body.Bytes(), []byte(`window.clarity("identify", "sess-123", "sess-123")`)) {
		t.Error("HTML should include Clarity identify call with session id")
	}
}

func TestFormatShoppingListHTML_NoClarityWhenEmpty(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	prev := templates.Clarityproject
	t.Cleanup(func() {
		templates.Clarityproject = prev
	})
	templates.Clarityproject = ""
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	assertHTTPSuccess(t, w)
	if bytes.Contains(w.Body.Bytes(), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

func TestFormatShoppingListHTML_IncludesGoogleTagManagerScript(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())

	prev := templates.GoogleTagManagerID
	t.Cleanup(func() {
		templates.GoogleTagManagerID = prev
	})
	templates.GoogleTagManagerID = "GTM-ABC123"
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	assertHTTPSuccess(t, w)
	if !bytes.Contains(w.Body.Bytes(), []byte("www.googletagmanager.com/gtm.js?id=")) {
		t.Error("HTML should contain Google Tag Manager script URL")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("'GTM-ABC123'")) {
		t.Error("HTML should contain Google Tag Manager ID")
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("www.googletagmanager.com/ns.html?id=GTM-ABC123")) {
		t.Error("HTML should contain Google Tag Manager noscript URL")
	}
}

func TestFormatShoppingListHTML_NoGoogleTagWhenEmpty(t *testing.T) {
	// Keep sequential: this test changes process-wide state.
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	prev := templates.GoogleTagManagerID
	t.Cleanup(func() {
		templates.GoogleTagManagerID = prev
	})
	templates.GoogleTagManagerID = ""
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	assertHTTPSuccess(t, w)
	if bytes.Contains(w.Body.Bytes(), []byte("googletagmanager.com")) {
		t.Error("HTML should not contain Google Tag Manager script when tag ID is empty")
	}
}

func TestFormatShoppingListHTML_HomePageLink(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, list, true, recipeSelection{}, w)
	html := assertHTTPSuccess(t, w)

	// Verify "Careme" is a link to home page
	if !strings.Contains(html, `<a href="/"`) {
		t.Error("HTML should contain a link to home page")
	}
	if !strings.Contains(html, "Careme</a>") {
		t.Error("HTML should contain 'Careme' as a link")
	}
}

func TestShoppingCardDetailsIndependentOfReadiness(t *testing.T) {
	t.Parallel()
	for _, ready := range []bool{false, true} {
		for _, content := range []string{"none", "ingredients", "instructions"} {
			t.Run(content+map[bool]string{false: " draft", true: " ready"}[ready], func(t *testing.T) {
				recipe := ai.Recipe{Title: "Dinner"}
				if content == "ingredients" {
					recipe.Ingredients = []ai.Ingredient{{Name: "Beans"}}
				}
				if content == "instructions" {
					recipe.Instructions = []string{"Cook the beans."}
				}
				view := shoppingRecipeView{Recipe: recipe, Hash: recipe.ComputeHash(), Ready: ready, ServerSignedIn: true}
				var body bytes.Buffer
				if err := templates.ShoppingList.ExecuteTemplate(&body, "shopping_recipe_card", view); err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, content != "none", strings.Contains(body.String(), "<details"))
				assert.Equal(t, ready, strings.Contains(body.String(), `hx-post="/recipe/`+view.Hash+`/save"`))
				assert.Equal(t, ready, strings.Contains(body.String(), `hx-post="/recipe/`+view.Hash+`/dismiss"`))
				assert.Equal(t, ready, strings.Contains(body.String(), `href="/recipe/`+view.Hash+`"`))
			})
		}
	}
}

func TestFormatShoppingListHTML_ShowsSaveButHidesOtherMutationsWhenSignedOut(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	multiRecipeList := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:       "Recipe One",
				Description: "First recipe",
				Ingredients: []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"}},
				Instructions: []string{
					"Step 1",
				},
				Health:       "Healthy",
				DrinkPairing: "Water",
			},
		},
	}

	w := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, multiRecipeList, false, recipeSelection{}, w)
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	if !strings.Contains(html, `/recipes/`) || !strings.Contains(html, `/regenerate"`) {
		t.Error("shopping list HTML should expose regenerate endpoint when signed out")
	}
	if !strings.Contains(html, `/recipe/`) || !strings.Contains(html, `/save"`) {
		t.Error("shopping list HTML should expose save endpoint when signed out")
	}
	if strings.Contains(html, `/recipe/`) && strings.Contains(html, `/dismiss"`) {
		t.Error("shopping list HTML should not expose dismiss endpoint when signed out")
	}
	if strings.Contains(html, `/recipes/`) && strings.Contains(html, `/finalize"`) {
		t.Error("shopping list HTML should not expose finalize endpoint when signed out")
	}
	if strings.Contains(html, `/recipe/`) && strings.Contains(html, `/wine?view=shopping`) {
		t.Error("shopping list HTML should not expose shopping wine endpoint when signed out")
	}
	if !strings.Contains(html, "Try again, chef") {
		t.Error("shopping list HTML should show regenerate action when signed out")
	}
	if !strings.Contains(html, "Build Shopping List") {
		t.Error("shopping list HTML should show finalize action when signed out")
	}
	if strings.Contains(html, `id="dismiss-`) {
		t.Error("shopping list HTML should hide dismiss controls when signed out")
	}
	if !strings.Contains(html, `href="/sign-in?return_to_b64=`) {
		t.Error("shopping list HTML should render a header sign-in link when signed out")
	}
}

func TestFormatShoppingListHTML_AllowsIngredientWithoutPrice(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	listWithoutPrice := ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:        "Spring Pasta",
				Description:  "Bright and quick",
				CookTime:     "20 minutes",
				CostEstimate: "$12-15",
				Ingredients: []ai.Ingredient{
					{Name: "English peas", Quantity: "1 cup", Price: ""},
				},
				Instructions: []string{"Boil and toss."},
				Health:       "Balanced",
				DrinkPairing: "Lemon water",
			},
		},
	}

	formatShoppingListHTMLForTest(t.Context(), p, listWithoutPrice, true, recipeSelection{}, w)
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)
	if !strings.Contains(html, "English peas") {
		t.Fatal("shopping list HTML should include ingredient name when price is empty")
	}
	if !strings.Contains(html, "1 cup") {
		t.Fatal("shopping list HTML should include ingredient quantity when price is empty")
	}
	if !strings.Contains(html, `hidden sm:block`) {
		t.Fatal("shopping list HTML should reserve desktop alignment when ingredient price is empty")
	}
}

func TestFormatShoppingListHTML_RendersRecipeImageInResponsiveQuarterWidthColumn(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	w := httptest.NewRecorder()
	recipeHash := list.Recipes[0].ComputeHash()

	writeShoppingListPage(t.Context(), w, shoppingListViewInput{
		params:              p,
		list:                list,
		wineRecommendations: nil,
		recipeImages:        map[string]bool{recipeHash: true},
		currentUser:         renderTestUser(true),
		hash:                p.Hash(),
		selection:           recipeSelection{},
		helpMessage:         "",
		pendingInstructions: "",
		progress:            shoppingProgress{},
	})
	html := assertHTTPSuccess(t, w)

	assert.Contains(t, html, `src="/recipe/`+recipeHash+`/image"`)
	assert.Contains(t, html, `sm:grid-cols-[minmax(0,1fr)_25%]`)
	assert.Contains(t, html, `w-1/4 shrink-0`)
	assert.Contains(t, html, `sm:row-span-3`)
}

func TestFormatShoppingListHTMLForHash_RendersWineOnlyInDetails(t *testing.T) {
	t.Parallel()
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
	selection := recipeSelection{SavedHashes: []string{wineHash}}
	w := httptest.NewRecorder()
	writeShoppingListPage(t.Context(), w, shoppingListViewInput{
		params: p,
		list:   multi,
		wineRecommendations: map[string]*ai.WineSelection{
			wineHash: {
				Wines: []ai.Ingredient{
					{Name: "Cellar Red", Quantity: "1 bottle", Price: "$15"},
					{Name: "Second Bottle", Quantity: "1 bottle", Price: "$18"},
				},
				Commentary: "Good with roasted flavors.",
			},
		},
		recipeImages:        nil,
		currentUser:         renderTestUser(true),
		hash:                p.Hash(),
		selection:           selection,
		helpMessage:         "",
		pendingInstructions: "",
		progress:            shoppingProgress{},
	})
	html := assertHTTPSuccess(t, w)

	isValidHTML(t, html)

	if strings.Contains(html, `aria-label="Choose wine"`) || strings.Contains(html, `/wine"`) || strings.Contains(html, `/wine?view=shopping`) {
		t.Fatalf("shopping list should not include wine picker controls, body: %s", html)
	}
	if got := strings.Count(html, "Cellar Red"); got != 3 {
		t.Fatalf("shopping list should show selected wine in ingredients, recommendation, and combined list; got count %d, body: %s", got, html)
	}
	if got := strings.Count(html, "Second Bottle"); got != 1 {
		t.Fatalf("shopping list should only add the second wine to recommendation; got count %d, body: %s", got, html)
	}
	if got := strings.Count(html, "Good with roasted flavors."); got != 1 {
		t.Fatalf("shopping list should render wine commentary once in details; got count %d, body: %s", got, html)
	}
	if strings.Index(html, "Drink pairing:") > strings.Index(html, "Good with roasted flavors.") {
		t.Fatalf("shopping list should render wine commentary beneath drink pairing, body: %s", html)
	}
}

func TestShoppingPageAndSelectionRenderSameCard(t *testing.T) {
	t.Parallel()
	recipe := list.Recipes[0]
	hash := recipe.ComputeHash()
	params := DefaultParams(&locations.Location{ID: "store"}, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	page, err := newShoppingListPageView(t.Context(), shoppingListViewInput{
		params:      params,
		list:        ai.ShoppingList{Recipes: []ai.Recipe{recipe}},
		hash:        "list-hash",
		currentUser: renderTestUser(true),
		selection:   selectionFromSaved([]ai.Recipe{recipe}),
	})
	if !assert.NoError(t, err) {
		return
	}
	var card, full, fragment bytes.Buffer
	assert.NoError(t, writeShoppingRecipeCard(&card, recipe, shoppingRecipeInput{
		ShoppingListHash: "list-hash",
		ServerSignedIn:   true,
		Saved:            true,
		Ready:            true,
	}))
	assert.NoError(t, templates.ShoppingList.ExecuteTemplate(&full, "shoppinglist.html", page))
	assert.NoError(t, templates.ShoppingList.ExecuteTemplate(&fragment, "shopping_content", page))
	assert.Contains(t, full.String(), card.String())
	assert.Contains(t, fragment.String(), card.String())
	assert.Equal(t, hash, page.Recipes[0].Hash)
}
