package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCocktailsRequestAndResponse(t *testing.T) {
	menu := CocktailMenu{}
	for _, title := range []string{"Pear Sour", "Pear Cooler", "Pear Smash"} {
		menu.Cocktails = append(menu.Cocktails, Cocktail{Title: title, Description: "A pear drink.", SeasonalNote: "Pears suit fall.", Glass: "Rocks", Ingredients: []Ingredient{{ProductID: "gin", Name: "Gin", Quantity: "1½ oz"}, {ProductID: "pear", Name: "Pear", Quantity: "½ pear"}}, Instructions: []string{"Muddle ½ pear, then shake with 1½ oz gin and strain."}})
	}
	menuJSON, err := json.Marshal(menu)
	require.NoError(t, err)
	transport := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, defaultRecipeModel, request["model"])
		assert.Equal(t, cocktailInstructions, request["instructions"])
		assert.Contains(t, request["input"], "Fall in Washington")
		assert.Contains(t, request["input"], "pear")
		format := schemaObject(t, schemaObject(t, request["text"])["format"])
		schema := schemaObject(t, format["schema"])
		drinks := schemaObject(t, schemaProperties(t, schema)["cocktails"])
		assert.Equal(t, float64(3), drinks["minItems"])
		assert.Equal(t, float64(3), drinks["maxItems"])
		properties := schemaProperties(t, schemaObject(t, drinks["items"]))
		assert.NotContains(t, properties, "wine_styles")
		assert.NotContains(t, properties, "drink_pairing")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"resp-cocktails","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]}`, string(menuJSON))))}, nil
	})}
	client := NewClient("test-key", "ignored", transport, nil)
	got, err := client.GenerateCocktails(t.Context(), "Fall in Washington", []InputIngredient{{ProductID: "gin"}, {ProductID: "pear"}})
	require.NoError(t, err)
	require.Len(t, got.Cocktails, 3)
	assert.Equal(t, "Pear Sour", got.Cocktails[0].Title)
}
