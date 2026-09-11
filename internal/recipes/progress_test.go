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
	assert.Contains(t, body, `id="shopping-slot-0"`)
	assert.Contains(t, body, `id="shopping-slot-1"`)
	assert.NotContains(t, body, `hx-post="/recipe/`)

	ready := ai.Recipe{Title: "Thai tofu", Ingredients: []ai.Ingredient{{Name: "Tofu"}}, Instructions: []string{"Cook."}, OriginHash: hash}
	draft := ai.Recipe{Title: "Draft beans", OriginHash: hash}
	require.NoError(t, s.SaveRecipe(t.Context(), ready))
	require.NoError(t, s.SaveRecipe(t.Context(), draft))
	require.NoError(t, statuses.RecipeReady(t.Context(), hash, 1, ready.ComputeHash()))
	body = poll(true)
	assert.Less(t, strings.Index(body, `id="shopping-slot-0"`), strings.Index(body, `id="shopping-recipe-`))
	assert.Contains(t, body, "hx-preserve")
	assert.Contains(t, body, `/recipe/`+ready.ComputeHash()+`/save`)
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
	assert.Contains(t, poll(false), "Recipe added")

	require.NoError(t, statuses.Fail(t.Context(), hash, errors.New("Recipe service unavailable")))
	body = poll(true)
	assert.Contains(t, body, ready.Title)
	assert.Contains(t, body, "Recipe service unavailable")
	assert.Contains(t, body, `/recipes/`+hash+`/retry`)
	assert.NotContains(t, body, `hx-trigger="every 1s"`)
	require.NoError(t, s.requireReadyRecipe(t.Context(), hash, ready.ComputeHash()))

	// A retry clears its progress but does not remove recipes already added.
	require.NoError(t, statuses.Start(t.Context(), hash))
	selection, err = s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	assert.Contains(t, selection.SavedHashes, ready.ComputeHash())
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{ready}, Plan: &ai.MenuPlan{Plans: plans}}, hash))
	body = poll(true)
	assert.NotContains(t, body, `hx-trigger="every 1s"`)
	assert.NotContains(t, body, "shopping-slot-")
	assert.Contains(t, body, `/recipes/`+hash+`/finalize`)
	assert.Contains(t, body, "Recipe added")
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
