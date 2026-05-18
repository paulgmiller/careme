package prompts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminMenuPromptJSON(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	require.NoError(t, recipes.IO(cacheStore).SaveShoppingList(t.Context(), &ai.ShoppingList{
		Plan: &ai.MenuPlan{ResponseID: "resp-menu-123"},
	}, "menu-hash"))
	require.NoError(t, NewCacheRecorder(cacheStore).RecordPrompt(t.Context(), &ai.PromptRecord{
		ResponseID:   "resp-menu-123",
		Model:        "gpt-menu",
		Instructions: "plan dinners",
		Input:        []ai.PromptMessage{{Role: "user", Content: "make three recipes"}},
	}))

	mux := http.NewServeMux()
	mux.Handle("/prompt/menu/{hash}", AdminMenuPromptJSON(cacheStore))
	req := httptest.NewRequest(http.MethodGet, "/prompt/menu/menu-hash", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rr.Body.String(), "\n  \"response_id\": \"resp-menu-123\"")
	assert.Contains(t, rr.Body.String(), "\n  \"instructions\": \"plan dinners\"")

	var got ai.PromptRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "resp-menu-123", got.ResponseID)
	assert.Equal(t, []ai.PromptMessage{{Role: "user", Content: "make three recipes"}}, got.Input)
}

func TestAdminRecipePromptJSON(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	recipe := ai.Recipe{
		Title:      "Tomato Pasta",
		ResponseID: "resp-recipe-123",
	}
	recipeHash := recipe.ComputeHash()
	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), recipe))
	require.NoError(t, NewCacheRecorder(cacheStore).RecordPrompt(t.Context(), &ai.PromptRecord{
		ResponseID:   "resp-recipe-123",
		Model:        "gpt-recipe",
		Instructions: "cook well",
		Input:        []ai.PromptMessage{{Role: "user", Content: "use tomatoes"}},
	}))

	mux := http.NewServeMux()
	mux.Handle("/prompt/recipe/{hash}", AdminRecipePromptJSON(cacheStore))
	req := httptest.NewRequest(http.MethodGet, "/prompt/recipe/"+recipeHash, nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rr.Body.String(), "\n  \"response_id\": \"resp-recipe-123\"")
	assert.Contains(t, rr.Body.String(), "\n  \"instructions\": \"cook well\"")

	var got ai.PromptRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "resp-recipe-123", got.ResponseID)
	assert.Equal(t, []ai.PromptMessage{{Role: "user", Content: "use tomatoes"}}, got.Input)
}

func TestAdminRecipePromptJSONPrependsParentPromptInputs(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	recipe := ai.Recipe{
		Title:      "Better Tomato Pasta",
		ResponseID: "resp-child",
	}
	recipeHash := recipe.ComputeHash()
	require.NoError(t, recipes.IO(cacheStore).SaveRecipe(t.Context(), recipe))

	recorder := NewCacheRecorder(cacheStore)
	require.NoError(t, recorder.RecordPrompt(t.Context(), &ai.PromptRecord{
		ResponseID: "resp-grandparent",
		Model:      "gpt-recipe",
		Input:      []ai.PromptMessage{{Role: "user", Content: "original dinner request"}},
	}))
	require.NoError(t, recorder.RecordPrompt(t.Context(), &ai.PromptRecord{
		ResponseID:         "resp-parent",
		Model:              "gpt-recipe",
		PreviousResponseID: "resp-grandparent",
		Input:              []ai.PromptMessage{{Role: "user", Content: "make it brighter"}},
	}))
	require.NoError(t, recorder.RecordPrompt(t.Context(), &ai.PromptRecord{
		ResponseID:         "resp-child",
		Model:              "gpt-recipe",
		PreviousResponseID: "resp-parent",
		Input:              []ai.PromptMessage{{Role: "user", Content: "make it faster"}},
	}))

	mux := http.NewServeMux()
	mux.Handle("/prompt/recipe/{hash}", AdminRecipePromptJSON(cacheStore))
	req := httptest.NewRequest(http.MethodGet, "/prompt/recipe/"+recipeHash, nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	var got ai.PromptRecord
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "resp-child", got.ResponseID)
	assert.Equal(t, "resp-parent", got.PreviousResponseID)
	assert.Equal(t, []ai.PromptMessage{
		{Role: "user", Content: "original dinner request"},
		{Role: "user", Content: "make it brighter"},
		{Role: "user", Content: "make it faster"},
	}, got.Input)
}

func TestAdminMenuPromptJSONMissingPrompt(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	require.NoError(t, recipes.IO(cacheStore).SaveShoppingList(t.Context(), &ai.ShoppingList{
		Plan: &ai.MenuPlan{ResponseID: "resp-missing"},
	}, "menu-hash"))

	mux := http.NewServeMux()
	mux.Handle("/prompt/menu/{hash}", AdminMenuPromptJSON(cacheStore))
	req := httptest.NewRequest(http.MethodGet, "/prompt/menu/menu-hash", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "prompt not found in cache")
}

func TestAdminRecipePromptJSONMissingRecipe(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle("/prompt/recipe/{hash}", AdminRecipePromptJSON(cache.NewFileCache(t.TempDir())))
	req := httptest.NewRequest(http.MethodGet, "/prompt/recipe/missing", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "load recipe: cache entry not found")
}
