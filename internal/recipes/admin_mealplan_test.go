package recipes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminMealPlanPageRendersCurrentPlan(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	rio := IO(cacheStore)
	params := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC))
	params.Instructions = "make it vegetarian"
	hash := params.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), params))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Korean", "tofu", hash), hash))

	rr := serveAdminMealPlanPage(t, rio, http.MethodGet, "/mealplan/"+hash)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Meal Plan Chain")
	assert.Contains(t, rr.Body.String(), "Korean")
	assert.Contains(t, rr.Body.String(), "tofu")
	assert.Contains(t, rr.Body.String(), "make it vegetarian")
	assert.Contains(t, rr.Body.String(), "/admin/params/"+hash)
	assert.Contains(t, rr.Body.String(), "Total plan entries: 1")
}

func TestAdminMealPlanPageWalksBackThroughSavedRecipeOrigins(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	rio := IO(cacheStore)

	ancestorParams := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 5, 0, 0, 0, 0, time.UTC))
	ancestorHash := ancestorParams.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), ancestorParams))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Thai", "chicken", ancestorHash), ancestorHash))

	currentParams := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC))
	currentParams.Saved = []ai.Recipe{testAdminMealPlanRecipe("Saved Thai Curry", ancestorHash)}
	currentHash := currentParams.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), currentParams))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Mexican", "beans", currentHash), currentHash))

	rr := serveAdminMealPlanPage(t, rio, http.MethodGet, "/mealplan/"+currentHash)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), currentHash)
	assert.Contains(t, rr.Body.String(), ancestorHash)
	assert.Contains(t, rr.Body.String(), "Mexican")
	assert.Contains(t, rr.Body.String(), "Thai")
	assert.Contains(t, rr.Body.String(), "Shopping lists visited: 2")
	assert.Contains(t, rr.Body.String(), "Total plan entries: 2")
}

func TestAdminMealPlanPageDeduplicatesSavedRecipeOrigins(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	rio := IO(cacheStore)

	ancestorParams := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 5, 0, 0, 0, 0, time.UTC))
	ancestorHash := ancestorParams.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), ancestorParams))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Thai", "chicken", ancestorHash), ancestorHash))

	currentParams := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC))
	currentParams.Saved = []ai.Recipe{
		testAdminMealPlanRecipe("Saved Thai Curry", ancestorHash),
		testAdminMealPlanRecipe("Saved Thai Soup", ancestorHash),
	}
	currentHash := currentParams.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), currentParams))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Mexican", "beans", currentHash), currentHash))

	rr := serveAdminMealPlanPage(t, rio, http.MethodGet, "/mealplan/"+currentHash)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 1, strings.Count(rr.Body.String(), "<code>"+ancestorHash+"</code>"))
	assert.Contains(t, rr.Body.String(), "Shopping lists visited: 2")
}

func TestAdminMealPlanPageWarnsForMissingAncestor(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	rio := IO(cacheStore)

	currentParams := testAdminMealPlanParams("70001001", "Test Store", time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC))
	currentParams.Saved = []ai.Recipe{testAdminMealPlanRecipe("Missing Origin Recipe", "missing-origin")}
	currentHash := currentParams.Hash()
	require.NoError(t, rio.SaveParams(t.Context(), currentParams))
	require.NoError(t, rio.SaveShoppingList(t.Context(), testAdminMealPlanList("Mexican", "beans", currentHash), currentHash))

	rr := serveAdminMealPlanPage(t, rio, http.MethodGet, "/mealplan/"+currentHash)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "shopping list not found: missing-origin")
	assert.Contains(t, rr.Body.String(), "Mexican")
}

func TestAdminMealPlanPageMissingStartHashReturnsNotFound(t *testing.T) {
	t.Parallel()

	rr := serveAdminMealPlanPage(t, IO(cache.NewFileCache(t.TempDir())), http.MethodGet, "/mealplan/missing")

	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "meal plan not found")
}

func TestAdminMealPlanPageRejectsNonGetHead(t *testing.T) {
	t.Parallel()

	rr := serveAdminMealPlanPage(t, IO(cache.NewFileCache(t.TempDir())), http.MethodPost, "/mealplan/abc")

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func serveAdminMealPlanPage(t *testing.T, rio recipeio, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("/mealplan/{hash}", AdminMealPlanPage(rio))
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func testAdminMealPlanParams(locationID, locationName string, date time.Time) *generatorParams {
	return DefaultParams(&locations.Location{
		ID:   locationID,
		Name: locationName,
	}, date)
}

func testAdminMealPlanList(cuisine, anchorIngredient, originHash string) *ai.ShoppingList {
	return &ai.ShoppingList{
		Recipes: []ai.Recipe{testAdminMealPlanRecipe(cuisine+" Recipe", originHash)},
		Plan: &ai.MenuPlan{
			ResponseID: "resp-menu-" + cuisine,
			Plans: []ai.RecipePlan{{
				Cuisine:            cuisine,
				AnchorIngredient:   anchorIngredient,
				Technique:          "stir-fry",
				SideVegetable:      "greens",
				RecipeInstructions: []string{"use the good pan"},
			}},
		},
	}
}

func testAdminMealPlanRecipe(title, originHash string) ai.Recipe {
	return ai.Recipe{
		Title:        title,
		Description:  "A test recipe",
		CookTime:     "30 minutes",
		CostEstimate: "$10",
		Ingredients:  []ai.Ingredient{{Name: "Ingredient", Quantity: "1 cup"}},
		Instructions: []string{"Cook it"},
		Health:       "Fine",
		DrinkPairing: "Water",
		OriginHash:   originHash,
		ResponseID:   "resp-" + title,
	}
}
