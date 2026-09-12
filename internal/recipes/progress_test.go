package recipes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Test Store"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	hash := p.Hash()
	statuses := s.generationStatuses.(*status.Store)
	require.NoError(t, statuses.Start(t.Context(), hash))
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
	assert.Contains(t, poll(false), "Planning your meals…")
	plans := []ai.RecipePlan{{Cuisine: "Italian", AnchorIngredient: "beans"}, {Cuisine: "Thai", AnchorIngredient: "tofu"}}
	require.NoError(t, statuses.Plan(t.Context(), hash, plans))
	body := poll(true)
	assert.NotContains(t, body, "<!doctype html>")
	assert.Contains(t, body, `id="shopping-recipe-pending-0"`)
	assert.Contains(t, body, `id="shopping-recipe-pending-1"`)
	assert.NotContains(t, body, `hx-post="/recipe/`)

	ready := ai.Recipe{Title: "Thai tofu", Properties: ai.RecipeProperties{TotalMinutes: 35}, Ingredients: []ai.Ingredient{{Name: "Tofu"}}, Instructions: []string{"Cook."}, OriginHash: hash}
	draft := ai.Recipe{Title: "Draft beans", OriginHash: hash}
	require.NoError(t, s.SaveRecipe(t.Context(), ready))
	require.NoError(t, s.SaveRecipe(t.Context(), draft))
	require.NoError(t, statuses.RecipeReady(t.Context(), hash, 1, ready.ComputeHash()))
	body = poll(true)
	assert.Less(t, strings.Index(body, "Italian with beans"), strings.Index(body, "Thai tofu"))
	assert.Contains(t, body, `id="shopping-recipe-pending-0"`)
	assert.Contains(t, body, `id="shopping-recipe-`+strings.TrimRight(ready.ComputeHash(), "=")+`"`)
	assert.Contains(t, body, "35 min")
	assert.Contains(t, body, `href="/recipe/`+ready.ComputeHash()+`"`)
	assert.NotContains(t, body, `href="/recipe/pending-0"`)
	assert.NotContains(t, body, "hx-preserve")
	assert.NotContains(t, body, "hx-sync")
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/save`)
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/dismiss`)
	assert.NotContains(t, body, `/recipe/pending-0/save`)
	assert.NotContains(t, body, `/recipe/`+draft.ComputeHash()+`/save`)
	assert.NotContains(t, body, `finalizeButton`)
	assert.NotContains(t, body, `/recipes/`+hash+`/finalize`)
	assert.ErrorIs(t, s.requireReadyRecipe(t.Context(), hash, draft.ComputeHash()), errRecipeNotReady)
	require.NoError(t, s.requireReadyRecipe(t.Context(), hash, ready.ComputeHash()))

	save := func(recipeHash string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(url.Values{"h": {hash}}.Encode()))
		req.SetPathValue("hash", recipeHash)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		s.handleSaveRecipe(rr, req)
		return rr
	}
	rejected := save(draft.ComputeHash())
	require.Equal(t, http.StatusConflict, rejected.Code)
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Empty(t, selection.SavedHashes)
	accepted := save(ready.ComputeHash())
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.NotContains(t, accepted.Body.String(), `/recipes/`+hash+`/finalize`)
	assert.Contains(t, poll(true), "Recipe added")
	dismiss := httptest.NewRequest(http.MethodPost, "/recipe/"+ready.ComputeHash()+"/dismiss", strings.NewReader(url.Values{"h": {hash}}.Encode()))
	dismiss.SetPathValue("hash", ready.ComputeHash())
	dismiss.Header.Set("HX-Request", "true")
	dismiss.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hidden := httptest.NewRecorder()
	s.handleDismissRecipe(hidden, dismiss)
	require.Equal(t, http.StatusOK, hidden.Code, hidden.Body.String())
	assert.Contains(t, hidden.Body.String(), "Restore")
	assert.NotContains(t, hidden.Body.String(), `/recipes/`+hash+`/finalize`)

	selection, err = s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Empty(t, selection.SavedHashes)
	assert.Equal(t, []string{ready.ComputeHash()}, selection.DismissedHashes)
	assert.Contains(t, poll(true), "Restore")

	require.NoError(t, statuses.Fail(t.Context(), hash, errors.New("Recipe service unavailable")))
	body = poll(true)
	assert.Contains(t, body, ready.Title)
	assert.Contains(t, body, "Recipe service unavailable")
	assert.Contains(t, body, `/recipes/`+hash+`/retry`)
	assert.NotContains(t, body, `hx-trigger="every 1s"`)
	require.NoError(t, s.requireReadyRecipe(t.Context(), hash, ready.ComputeHash()))

	// A retry clears progress; previously published recipes are no longer selectable.
	require.NoError(t, statuses.Start(t.Context(), hash))
	selection, err = s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Empty(t, selection.SavedHashes)
	require.ErrorIs(t, s.requireReadyRecipe(t.Context(), hash, ready.ComputeHash()), errRecipeNotReady)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{ready}, Plan: &ai.MenuPlan{Plans: plans}}, hash))
	body = poll(true)
	assert.NotContains(t, body, `hx-trigger="every 1s"`)
	assert.NotContains(t, body, "shopping-recipe-pending-")
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/save`)
	require.NoError(t, s.requireReadyRecipe(t.Context(), hash, ready.ComputeHash()))
	accepted = save(ready.ComputeHash())
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	assert.Contains(t, accepted.Body.String(), `/recipes/`+hash+`/finalize`)
	assert.Contains(t, poll(false), "Recipe added")
	assert.ErrorIs(t, s.requireReadyRecipe(t.Context(), hash, draft.ComputeHash()), errRecipeNotReady)
	s.Wait()
}

func TestShoppingProgressKeepsSavedRecipesDuringReplacement(t *testing.T) {
	s := newTestServer(t)
	saved := ai.Recipe{Title: "Already added", Instructions: []string{"Cook."}}
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{saved}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.generationStatuses.Start(t.Context(), p.Hash()))
	require.NoError(t, s.generationStatuses.(*status.Store).Plan(t.Context(), p.Hash(), []ai.RecipePlan{{Cuisine: "French", AnchorIngredient: "beans"}}))
	rr := httptest.NewRecorder()
	s.handleRecipes(rr, httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), saved.Title)
	assert.Contains(t, rr.Body.String(), `href="/recipe/`+saved.ComputeHash()+`"`)
	assert.Contains(t, rr.Body.String(), `/recipe/`+saved.ComputeHash()+`/dismiss`)
	assert.Contains(t, rr.Body.String(), "Recipe added")
	assert.NotContains(t, rr.Body.String(), `/recipes/`+p.Hash()+`/finalize`)
	require.NoError(t, s.requireReadyRecipe(t.Context(), p.Hash(), saved.ComputeHash()))
	_, err := s.FromCache(t.Context(), p.Hash())
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestReadySingleRecipeReplacementRequiresCompletedJob(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	original := ai.Recipe{Title: "Original", OriginHash: p.Hash()}
	replacement := ai.Recipe{Title: "Replacement", OriginHash: p.Hash(), ParentHash: original.ComputeHash()}
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{original}}, p.Hash()))
	require.NoError(t, s.SaveRecipe(t.Context(), replacement))
	require.NoError(t, s.SaveThread(t.Context(), original.ComputeHash(), []RecipeThreadEntry{{ResponseID: "answer"}}))
	id := status.ID(original.ComputeHash(), "answer")
	require.NoError(t, s.generationStatuses.Start(t.Context(), id))
	require.ErrorIs(t, s.requireReadyRecipe(t.Context(), p.Hash(), replacement.ComputeHash()), errRecipeNotReady)
	require.NoError(t, s.generationStatuses.Complete(t.Context(), id, replacement.ComputeHash()))
	require.NoError(t, s.requireReadyRecipe(t.Context(), p.Hash(), replacement.ComputeHash()))
}

func TestAddingRecipeDuringGenerationCompletionPreservesProfile(t *testing.T) {
	s := newTestServer(t)
	user := &utypes.User{ID: "progress-user", Email: []string{"progress@example.com"}, ShoppingDay: "Saturday"}
	require.NoError(t, s.storage.Update(user))
	location := &locations.Location{ID: "70000123", Name: "Store"}
	recipe := ai.Recipe{Title: "Ready dinner"}
	var wg sync.WaitGroup
	userID := user.ID
	wg.Go(func() { assert.NoError(t, s.recordShoppingListForUser(userID, "complete-list", location)) })
	wg.Go(func() { assert.NoError(t, s.saveRecipesToUserProfile(t.Context(), user, recipe)) })
	wg.Wait()
	got, err := s.storage.GetByID(user.ID)
	require.NoError(t, err)
	require.Len(t, got.ShoppingLists, 1)
	require.Len(t, got.LastRecipes, 1)
	assert.Equal(t, "complete-list", got.ShoppingLists[0].Hash)
	assert.Equal(t, recipe.ComputeHash(), got.LastRecipes[0].Hash)
}

func TestShoppingRecipeDOMIDsExcludeHashPadding(t *testing.T) {
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
