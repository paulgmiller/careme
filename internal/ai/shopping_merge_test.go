package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeShoppingQuantitiesSendsIngredientsAndCatalog(t *testing.T) {
	ingredients := []Ingredient{{ProductID: "pork", Name: "Ground pork", Quantity: "8 oz"}, {ProductID: "pork", Name: "Ground pork", Quantity: "16 oz"}, {ProductID: "garlic", Name: "Garlic", Quantity: "3 cloves"}}
	price := float32(6)
	catalog := []InputIngredient{{ProductID: "pork", Description: "Ground pork", Size: "32 oz", AisleNumber: "Meat", PriceRegular: &price, Grade: &IngredientGrade{Score: 10}, Embedding: IngredientEmbedding{1, 2}}, {ProductID: "garlic", Description: "Garlic", Size: "1 bulb"}}
	output := `{"items":[{"id":"pork","name":"Ground pork","needed":"24 oz","buy":1},{"id":"garlic","name":"Garlic","needed":"3 cloves","buy":null}]}`
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model        string `json:"model"`
			Instructions string `json:"instructions"`
			Reasoning    struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Text struct {
				Format map[string]any `json:"format"`
			} `json:"text"`
			Input []struct {
				Content string `json:"content"`
			} `json:"input"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, gpt56Luna, body.Model)
		assert.Equal(t, "none", body.Reasoning.Effort)
		assert.Contains(t, body.Instructions, "JSON number")
		assert.Contains(t, body.Instructions, "conversion is uncertain")
		assert.Equal(t, "json_schema", body.Text.Format["type"])
		assert.Equal(t, true, body.Text.Format["strict"])
		schema := schemaObject(t, body.Text.Format["schema"])
		assert.Equal(t, []string{"items"}, schemaRequired(t, schema))
		item := schemaObject(t, schemaObject(t, schemaProperties(t, schema)["items"])["items"])
		assert.ElementsMatch(t, []string{"id", "name", "needed", "buy"}, schemaRequired(t, item))
		assert.JSONEq(t, `{"anyOf":[{"type":"number"},{"type":"null"}]}`, mustJSON(t, schemaProperties(t, item)["buy"]))
		assert.NotContains(t, schemaProperties(t, item), "aisle_number")
		assert.NotContains(t, schemaProperties(t, item), "price")
		assert.NotContains(t, schemaProperties(t, item), "variant_id", "the selected catalog variant is attached by the server")
		require.Len(t, body.Input, 1)
		var input struct {
			Ingredients []Ingredient      `json:"ingredients"`
			Catalog     []InputIngredient `json:"catalog"`
		}
		require.NoError(t, json.Unmarshal([]byte(body.Input[0].Content), &input))
		assert.Equal(t, ingredients, input.Ingredients)
		expected := append([]InputIngredient{}, catalog...)
		expected[0].Grade = nil
		expected[0].Embedding = nil
		assert.Equal(t, expected, input.Catalog)
		return shoppingMergeHTTPResponse(req, output), nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), ingredients, catalog)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ShoppingItem{ProductID: "pork", Name: "Ground pork", Needed: "24 oz", Buy: new(float64(1))}, got[0])
	assert.Equal(t, ShoppingItem{ProductID: "garlic", Name: "Garlic", Needed: "3 cloves"}, got[1])
	assert.NotNil(t, catalog[0].Grade)
	assert.Equal(t, IngredientEmbedding{1, 2}, catalog[0].Embedding)
}

func TestMergeShoppingPurchaseResults(t *testing.T) {
	for _, tc := range []struct {
		name, output, size, wantErr string
		buy                         *float64
	}{
		{name: "whole size multiple", output: `{"items":[{"id":"pork","name":"Pork","needed":"40 oz","buy":3}]}`, size: "16 oz", buy: new(float64(3))},
		{name: "weight fraction", output: `{"items":[{"id":"pork","name":"Pork","needed":"8 oz","buy":0.5}]}`, size: "1 lb", buy: new(0.5)},
		{name: "pantry", output: `{"items":[{"id":"","name":"Saffron","needed":"1 pinch","buy":null}]}`},
		{name: "uncertain conversion", output: `{"items":[{"id":"pork","name":"Pork","needed":"1 cup","buy":null}]}`, size: "16 oz"},
		{name: "unknown product", output: `{"items":[{"id":"invented","name":"Pork","needed":"24 oz","buy":1}]}`, wantErr: "unknown product"},
		{name: "pantry cannot buy", output: `{"items":[{"id":"","name":"Saffron","needed":"1 pinch","buy":1}]}`, wantErr: "requires a catalog product"},
		{name: "missing size", output: `{"items":[{"id":"pork","name":"Pork","needed":"24 oz","buy":1}]}`, buy: new(float64(1))},
		{name: "zero buy", output: `{"items":[{"id":"pork","name":"Pork","needed":"24 oz","buy":0}]}`, size: "16 oz", wantErr: "must be positive"},
		{name: "negative buy", output: `{"items":[{"id":"pork","name":"Pork","needed":"24 oz","buy":-1}]}`, size: "16 oz", wantErr: "must be positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return shoppingMergeHTTPResponse(req, tc.output), nil })}, nil)
			got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{ProductID: "pork", Name: "Pork", Quantity: "24 oz"}}, []InputIngredient{{ProductID: "pork", Description: "Pork", Size: tc.size}})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tc.buy, got[0].Buy)
		})
	}
}

func TestMergeShoppingQuantitiesDecodeFailure(t *testing.T) {
	for _, output := range []string{`invalid`, `[]`, `{"items":[{"id":"","name":"","needed":3,"buy":null}]}`, `{"items":"invalid"}`, `{"items":[{"id":"","name":"Oil","needed":"4 tbsp","buy":"1 lb"}]}`} {
		t.Run(output, func(t *testing.T) {
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return shoppingMergeHTTPResponse(req, output), nil
			})}, nil)
			got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{Name: "Garlic", Quantity: "1 clove"}}, []InputIngredient{})
			require.ErrorContains(t, err, "decode shopping ingredients")
			assert.Nil(t, got)
		})
	}
}

func TestMergeShoppingQuantitiesAPIFailure(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid request","type":"invalid_request_error"}}`)), Request: req}, nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{Name: "Garlic", Quantity: "1 clove"}}, []InputIngredient{})
	require.ErrorContains(t, err, "merge shopping quantities")
	assert.Nil(t, got)
}

func TestMergeShoppingQuantitiesEmptyInput(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("empty ingredients should not call AI")
		return nil, nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{}, []InputIngredient{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func shoppingMergeHTTPResponse(req *http.Request, output string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
		"id":"resp-merge","object":"response","created_at":1778529600,"status":"completed","model":%q,
		"output":[{"id":"msg-merge","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":%q,"annotations":[]}]}],
		"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}
	}`, gpt56Luna, output))), Request: req}
}

func TestMatchingShoppingCatalog(t *testing.T) {
	needs := []Ingredient{{ProductID: "onion", Name: "Onion", Quantity: "4 oz"}, {ProductID: "onion", Name: "Onions", Quantity: "4 oz"}, {Name: "Olive oil", Quantity: "1 tbsp"}, {ProductID: "missing", Name: "Missing", Quantity: "1"}}
	catalog := []InputIngredient{{ProductID: "onion", Description: "Onion", Size: "1 lb"}, {ProductID: "other-onion", Description: "Onion", Size: "2 lb"}, {ProductID: "oil", Description: "Olive oil"}, {Description: "Unknown product"}}
	got := MatchingShoppingCatalog(needs, catalog)
	assert.Equal(t, []InputIngredient{catalog[0]}, got)
	assert.Len(t, catalog, 4)
	assert.Len(t, needs, 4)
	assert.Empty(t, MatchingShoppingCatalog(nil, catalog))
}
