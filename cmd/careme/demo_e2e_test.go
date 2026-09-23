package main

import (
	"encoding/json"
	"net/url"
	"slices"
	"testing"

	"careme/internal/ai"
	"careme/internal/demo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDemoDemo(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	client := newTestClient(t)
	response := mustGet(t, client, srv.URL+"/demo/mnfood/ingredients")
	defer func() { assert.NoError(t, response.Body.Close()) }()
	require.Equal(t, 200, response.StatusCode)
	require.Contains(t, response.Header.Get("Content-Type"), "application/json")
	body := readAll(t, response.Body)
	var ingredients []ai.InputIngredient
	require.NoError(t, json.Unmarshal([]byte(body), &ingredients))
	expected, err := (demo.Provider{}).FetchStaples(t.Context(), demo.LocationID)
	require.NoError(t, err)
	assert.Equal(t, expected, ingredients)
	body = mustGetBody(t, client, srv.URL+"/demo/mnfood")
	for _, item := range ingredients {
		if slices.Contains(item.Categories, "Produce") {
			assert.Contains(t, body, "<li>"+item.Description+"</li>")
		}
	}
	assert.Contains(t, body, "mnfood.club Butternut Squash")
	assert.Contains(t, body, "mnfood.club Blueberries")
	assert.Contains(t, body, `method="POST" action="/recipes"`)
	assert.Contains(t, body, `value="`+demo.LocationID+`"`)
	assert.Contains(t, body, "Recommended add-on")
	assert.Contains(t, body, "Meats are from TC Farm.")
	assert.NotContains(t, body, "Careme × TC Farm")
	assert.Contains(t, body, "Oddbird GSM NA Red Wine (dealcoholized)")
	recipeURL := mustStartRecipeGeneration(t, client, srv.URL+"/recipes", url.Values{"location": {demo.LocationID}})
	_, body = followUntilRecipes(t, client, recipeURL, true)
	assert.Contains(t, body, "mnfood.club")
	assert.NotEmpty(t, extractRecipeHashes(t, body))
}
