package main

import (
	"encoding/json"
	"net/url"
	"testing"

	"careme/internal/ai"
	"careme/internal/tcfarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCFarmDemo(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	client := newTestClient(t)
	response := mustGet(t, client, srv.URL+"/demo/tcfarm/ingredients")
	defer func() { assert.NoError(t, response.Body.Close()) }()
	require.Equal(t, 200, response.StatusCode)
	require.Contains(t, response.Header.Get("Content-Type"), "application/json")
	body := readAll(t, response.Body)
	var ingredients []ai.InputIngredient
	require.NoError(t, json.Unmarshal([]byte(body), &ingredients))
	expected, err := (tcfarm.Provider{}).FetchStaples(t.Context(), tcfarm.LocationID)
	require.NoError(t, err)
	assert.Equal(t, expected, ingredients)
	body = mustGetBody(t, client, srv.URL+"/demo/tcfarm")
	assert.Contains(t, body, "TC Farm")
	assert.Contains(t, body, `method="POST" action="/recipes"`)
	assert.Contains(t, body, `value="`+tcfarm.LocationID+`"`)
	assert.Contains(t, body, "Recommended add-on")
	assert.Contains(t, body, "Oddbird GSM NA Red Wine (dealcoholized)")
	recipeURL := mustStartRecipeGeneration(t, client, srv.URL+"/recipes", url.Values{"location": {tcfarm.LocationID}})
	_, body = followUntilRecipes(t, client, recipeURL, true)
	assert.Contains(t, body, "TC Farm")
	assert.NotEmpty(t, extractRecipeHashes(t, body))
}
