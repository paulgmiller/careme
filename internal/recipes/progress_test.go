package recipes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	utypes "careme/internal/users/types"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShoppingProgressReadinessAndCompletion(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Test Store"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	hash := p.Hash()
	statuses := s.generationStatuses.(*status.Store)
	require.NoError(t, statuses.Start(t.Context(), hash, status.InitialMessage))
	poll := func(fragment bool) string {
		req := httptest.NewRequest(http.MethodGet, "/recipes?h="+hash+"&help=Welcome", nil)
		if fragment {
			req.Header.Set("HX-Request", "true")
			req.Header.Set("HX-Target", "shopping-content")
		}
		rr := httptest.NewRecorder()
		s.handleRecipes(rr, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		return rr.Body.String()
	}
	body := poll(false)
	assert.Contains(t, body, "Your meals are taking shape. You can add finished recipes as they arrive.")
	assert.Contains(t, body, `hx-get="" hx-trigger="every 1s"`)
	assert.Contains(t, body, "animate-spin")
	message := "Considering 12 out of 30 ingredients\nCarrots & greens <fresh>"
	require.NoError(t, statuses.Update(t.Context(), hash, message))
	for _, fragment := range []bool{false, true} {
		body = poll(fragment)
		assert.Contains(t, body, "Considering 12 out of 30 ingredients\nCarrots &amp; greens &lt;fresh&gt;")
		assert.Less(t, strings.Index(body, "Considering 12"), strings.Index(body, status.InitialMessage))
		assert.Contains(t, body, "whitespace-pre-line")
	}
	plans := []ai.RecipePlan{
		{Cuisine: "Italian", DishFormat: "stew", AnchorIngredient: "beans", SideVegetable: "kale"},
		{Cuisine: "Thai", DishFormat: "stir-fry", AnchorIngredient: "tofu", SideVegetable: "broccoli"},
	}
	require.NoError(t, statuses.Plan(t.Context(), hash, plans))
	body = poll(true)
	assert.NotContains(t, body, "<!doctype html>")
	assert.Contains(t, body, `id="shopping-recipe-pending-0"`)
	assert.Contains(t, body, `id="shopping-recipe-pending-1"`)
	assert.NotContains(t, body, `hx-post="/recipe/`)
	assert.NotContains(t, body, `href="/recipe/`)

	ready := ai.Recipe{Title: "Thai tofu", Properties: ai.RecipeProperties{TotalMinutes: 35}, Ingredients: []ai.Ingredient{{Name: "Tofu"}}, Instructions: []string{"Cook."}, OriginHash: hash}
	draft := ai.Recipe{Title: "Draft beans", OriginHash: hash}
	require.NoError(t, s.SaveRecipe(t.Context(), ready))
	require.NoError(t, s.SaveRecipe(t.Context(), draft))
	require.NoError(t, statuses.RecipeReady(t.Context(), hash, 1, ready.ComputeHash()))
	body = poll(true)
	require.Contains(t, body, "Italian  stew")
	require.Contains(t, body, "using beans and kale")
	require.Contains(t, body, ready.Title)
	assert.Less(t, strings.Index(body, "Italian  stew"), strings.Index(body, ready.Title))
	assert.Contains(t, body, `id="shopping-recipe-pending-0"`)
	assert.Contains(t, body, `id="shopping-recipe-`+strings.TrimRight(ready.ComputeHash(), "=")+`"`)
	assert.Contains(t, body, "35 min")
	assert.Contains(t, body, `href="/recipe/`+ready.ComputeHash()+`"`)
	assert.NotContains(t, body, `href="/recipe/pending-0"`)
	assert.NotContains(t, body, `href="/recipe/`+draft.ComputeHash()+`"`)
	assert.Contains(t, body, `id="shopping-recipe-`+strings.TrimRight(ready.ComputeHash(), "=")+`-details" hx-preserve`)
	assert.NotContains(t, body, "hx-sync")
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/save`)
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/dismiss`)
	assert.NotContains(t, body, `/recipe/pending-0/save`)
	assert.NotContains(t, body, `/recipe/`+draft.ComputeHash()+`/save`)
	assert.NotContains(t, body, `finalizeButton`)
	assert.NotContains(t, body, `/recipes/`+hash+`/finalize`)

	save := func(recipeHash string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(url.Values{"h": {hash}}.Encode()))
		req.SetPathValue("hash", recipeHash)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		s.handleSaveRecipe(rr, req)
		return rr
	}
	accepted := save(ready.ComputeHash())
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Contains(t, accepted.Body.String(), `/recipes/`+hash+`/finalize`)
	assert.Contains(t, poll(true), "Recipe added")
	dismiss := httptest.NewRequest(http.MethodPost, "/recipe/"+ready.ComputeHash()+"/dismiss", strings.NewReader(url.Values{"h": {hash}}.Encode()))
	dismiss.SetPathValue("hash", ready.ComputeHash())
	dismiss.Header.Set("HX-Request", "true")
	dismiss.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hidden := httptest.NewRecorder()
	s.handleDismissRecipe(hidden, dismiss)
	require.Equal(t, http.StatusOK, hidden.Code, hidden.Body.String())
	assert.Contains(t, hidden.Body.String(), "Restore")
	assert.Contains(t, hidden.Body.String(), `/recipes/`+hash+`/finalize`)

	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Empty(t, selection.SavedHashes)
	assert.Equal(t, []string{ready.ComputeHash()}, selection.DismissedHashes)
	assert.Contains(t, poll(true), "Restore")

	require.NoError(t, statuses.Fail(t.Context(), hash, errors.New("Recipe service unavailable")))
	failedPoll := httptest.NewRequest(http.MethodGet, "/recipes?h="+hash+"&help=Welcome", nil)
	failedPoll.Header.Set("HX-Request", "true")
	failedPoll.Header.Set("HX-Target", "shopping-content")
	failedResponse := httptest.NewRecorder()
	s.handleRecipes(failedResponse, failedPoll)
	require.Equal(t, http.StatusOK, failedResponse.Code)
	assert.Equal(t, failedPoll.URL.RequestURI(), failedResponse.Header().Get("HX-Redirect"))
	assert.Empty(t, failedResponse.Body.String())
	body = poll(false)
	assert.Contains(t, body, `id="spin-page-work"`)
	assert.Contains(t, body, "Recipe service unavailable")
	assert.Contains(t, body, `/recipes/`+hash+`/retry`)
	assert.NotContains(t, body, `help=Welcome`)
	assert.NotContains(t, body, `id="shopping-content"`)

	// A retry clears progress and removes previously published cards.
	require.NoError(t, statuses.Start(t.Context(), hash, ""))
	assert.NotContains(t, poll(true), `href="/recipe/`)
	selection, err = s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Empty(t, selection.SavedHashes)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{ready}, Plan: &ai.MenuPlan{Plans: plans}}, hash))
	body = poll(true)
	assert.NotContains(t, body, "<!doctype html>")
	assert.Contains(t, body, `id="shopping-content"`)
	assert.NotContains(t, body, `hx-trigger="every 1s"`)
	assert.NotContains(t, body, "Considering 12 out of 30 ingredients")
	assert.NotContains(t, body, "shopping-recipe-pending-")
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/save`)
	accepted = save(ready.ComputeHash())
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Contains(t, accepted.Body.String(), `/recipes/`+hash+`/finalize`)
	assert.Contains(t, poll(false), "Recipe added")
	s.Wait()
}

func TestShoppingProgressKeepsSavedRecipesDuringReplacement(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	saved := ai.Recipe{Title: "Already added", Instructions: []string{"Cook."}}
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{saved}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.generationStatuses.Start(t.Context(), p.Hash(), ""))
	require.NoError(t, s.generationStatuses.(*status.Store).Plan(t.Context(), p.Hash(), []ai.RecipePlan{{Cuisine: "French", AnchorIngredient: "beans"}}))
	rr := httptest.NewRecorder()
	s.handleRecipes(rr, httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), saved.Title)
	assert.Contains(t, rr.Body.String(), `href="/recipe/`+saved.ComputeHash()+`"`)
	assert.Contains(t, rr.Body.String(), `/recipe/`+saved.ComputeHash()+`/dismiss`)
	assert.Contains(t, rr.Body.String(), "Recipe added")
	assert.NotContains(t, rr.Body.String(), `/recipes/`+p.Hash()+`/finalize`)
	_, err := s.FromCache(t.Context(), p.Hash())
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestAddingRecipeAfterGenerationCompletionPreservesProfile(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	user := &utypes.User{ID: "progress-user", Email: []string{"progress@example.com"}, ShoppingDay: "Saturday"}
	require.NoError(t, s.storage.Update(user))
	location := &locations.Location{ID: "70000123", Name: "Store"}
	recipe := ai.Recipe{Title: "Ready dinner"}
	require.NoError(t, s.recordShoppingListForUser(user.ID, "complete-list", location))
	// A save request loads the current profile after generation completes.
	user, err := s.storage.GetByID(user.ID)
	require.NoError(t, err)
	require.NoError(t, s.saveRecipesToUserProfile(t.Context(), user, recipe))
	got, err := s.storage.GetByID(user.ID)
	require.NoError(t, err)
	require.Len(t, got.ShoppingLists, 1)
	require.Len(t, got.LastRecipes, 1)
	assert.Equal(t, "complete-list", got.ShoppingLists[0].Hash)
	assert.Equal(t, recipe.ComputeHash(), got.LastRecipes[0].Hash)
	assert.Equal(t, user.Email, got.Email)
	assert.Equal(t, "Saturday", got.ShoppingDay)
}

func TestShoppingRecipeDOMIDsExcludeHashPadding(t *testing.T) {
	t.Parallel()
	recipe := ai.Recipe{Title: "Padded hash recipe", Instructions: []string{"Cook."}}
	hash := recipe.ComputeHash()
	require.True(t, strings.HasSuffix(hash, "=="), "regression requires a padded hash")
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	rr := httptest.NewRecorder()
	formatShoppingListHTMLForTest(t.Context(), p, ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, true, recipeSelection{}, rr)
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, `id="shopping-recipe-`+strings.TrimRight(hash, "=")+`"`)
	assert.NotContains(t, body, `id="shopping-recipe-`+hash+`"`)
	assert.Contains(t, body, `/recipe/`+hash+`/save`, "recipe identity must retain padding")
	assert.Contains(t, body, `/recipe/`+hash+`/dismiss`)
}

func TestShoppingProgressOrdersCardsBySlotHash(t *testing.T) {
	t.Parallel()
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	first := ai.Recipe{Title: "First ready recipe"}
	last := ai.Recipe{Title: "Last ready recipe"}
	saved := ai.Recipe{Title: "Previously saved recipe"}
	p.Saved = []ai.Recipe{saved}
	// The stub list contains only saved recipes; slots supply generated cards.
	list := ai.ShoppingList{Recipes: []ai.Recipe{saved}}
	finished := map[string]ai.Recipe{
		first.ComputeHash(): first,
		last.ComputeHash():  last,
	}
	progress := shoppingProgress{
		Finished:   finished,
		Generating: true,
		Fragment:   true,
		Slots: []status.Slot{
			{RecipeHash: first.ComputeHash()},
			{Plan: ai.RecipePlan{Cuisine: "Thai", DishFormat: "stir-fry", AnchorIngredient: "tofu", SideVegetable: "broccoli"}},
			{RecipeHash: last.ComputeHash()},
		},
	}
	rr := httptest.NewRecorder()
	writeShoppingListPage(t.Context(), rr, shoppingListViewInput{
		params:              p,
		list:                list,
		wineRecommendations: map[string]*ai.WineSelection{},
		recipeImages:        map[string]bool{},
		currentUser:         &utypes.User{ID: "test-user"},
		hash:                p.Hash(),
		selection:           selectionFromSaved(p.Saved),
		helpMessage:         "",
		pendingInstructions: "",
		progress:            progress,
	})
	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "using tofu and broccoli")
	titles := []string{first.Title, "Thai  stir-fry", last.Title, saved.Title}
	for _, title := range titles {
		require.Contains(t, body, title)
	}
	for i := 1; i < len(titles); i++ {
		assert.Less(t, strings.Index(body, titles[i-1]), strings.Index(body, titles[i]))
	}
	assert.NotContains(t, body, `href="/recipe/pending-1"`)
	assert.Contains(t, body, "Recipe added")
}
