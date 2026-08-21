package main

import (
	"bytes"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWriteEvalCasesWritesOneCasePerStoredRecipePlan(t *testing.T) {
	store, hash := seedEvalCaseStore(t)
	var out bytes.Buffer

	err := writeEvalCases(t.Context(), &out, store, hash)
	require.NoError(t, err)

	var tests []struct {
		Description string     `yaml:"description"`
		Vars        recipeVars `yaml:"vars"`
	}
	require.NoError(t, yaml.Unmarshal(out.Bytes(), &tests))
	require.Len(t, tests, 2)
	assert.Contains(t, tests[0].Description, hash)
	assert.Equal(t, "resp-menu-1", tests[0].Vars.MenuPlan.ResponseID)
	assert.Equal(t, "careme:store-day:v1:test", tests[0].Vars.MenuPlan.PromptCacheKey)
	require.Len(t, tests[0].Vars.MenuPlan.Plans, 1)
	assert.Equal(t, "Chicken Thighs", tests[0].Vars.MenuPlan.Plans[0].AnchorIngredient)
	require.Len(t, tests[1].Vars.MenuPlan.Plans, 1)
	assert.Equal(t, "Black Beans", tests[1].Vars.MenuPlan.Plans[0].AnchorIngredient)
	assert.Contains(t, out.String(), "anchor_ingredient: Chicken Thighs")
	assert.Contains(t, out.String(), "response_id: resp-menu-1")
	assert.NotContains(t, out.String(), "anchoringredient")
}

func TestParseOptionsAcceptsSecretFile(t *testing.T) {
	options, err := parseOptions([]string{
		"-hash", " shopping-list-hash ",
		"-secret-file", " secrets/prod ",
	}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "shopping-list-hash", options.Hash)
	assert.Equal(t, "secrets/prod", options.SecretFile)
}

func TestParseOptionsRequiresHash(t *testing.T) {
	options, err := parseOptions(nil, &bytes.Buffer{})

	assert.Empty(t, options)
	require.EqualError(t, err, "must provide -hash")
}

func seedEvalCaseStore(t *testing.T) (evalCaseStore, string) {
	t.Helper()

	cacheStore := cache.NewInMemoryCache()
	store := recipes.IO(cacheStore)
	params := recipes.DefaultParams(&locations.Location{
		ID:    "70001001",
		Name:  "Test Store",
		State: "WA",
	}, time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC))
	hash := params.Hash()
	require.NoError(t, store.SaveParams(t.Context(), params))
	require.NoError(t, store.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Plan: &ai.MenuPlan{
			Plans: []ai.RecipePlan{
				{Cuisine: "Italian", AnchorIngredient: "Chicken Thighs", Technique: "sheet pan", SideVegetable: "Broccoli"},
				{Cuisine: "Mexican", AnchorIngredient: "Black Beans", Technique: "simmer", SideVegetable: "Peppers", Fancy: true},
			},
			ChefNoteSuggestion: "faster dinners",
			ResponseID:         "resp-menu-1",
			PromptCacheKey:     "careme:store-day:v1:test",
		},
	}, hash))
	return store, hash
}
