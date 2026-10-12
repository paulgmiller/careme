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
	ingredients := []Ingredient{{ProductID: "small", Name: "Ground pork", Quantity: "8 oz"}, {ProductID: "small", Name: "Ground pork", Quantity: "16 oz"}, {Name: "Garlic", Quantity: "3 cloves"}}
	price := float32(6)
	catalog := []InputIngredient{{ProductID: "large", Description: "Ground pork", Size: "32 oz", AisleNumber: "Meat", PriceRegular: &price, Grade: &IngredientGrade{Score: 10}, Embedding: IngredientEmbedding{1, 2}}, {ProductID: "garlic", Description: "Garlic", Size: "1 bulb"}}
	output := `{"items":[{"needed":{"id":"small","name":"Ground pork","quantity":"24 oz","aisle_number":"","price":""},"product_id":"large","buy_quantity":"1 package"},{"needed":{"id":"","name":"Garlic","quantity":"3 cloves","aisle_number":"","price":""},"product_id":"garlic","buy_quantity":""}]}`
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
		assert.Contains(t, body.Instructions, "least excess")
		assert.Contains(t, body.Instructions, "total purchase price")
		assert.Contains(t, body.Instructions, "sale prices")
		assert.Contains(t, body.Instructions, "conversion is uncertain")
		assert.Equal(t, "json_schema", body.Text.Format["type"])
		assert.Equal(t, true, body.Text.Format["strict"])
		schema := schemaObject(t, body.Text.Format["schema"])
		assert.Equal(t, []string{"items"}, schemaRequired(t, schema))
		item := schemaObject(t, schemaObject(t, schemaProperties(t, schema)["items"])["items"])
		assert.ElementsMatch(t, []string{"needed", "product_id", "buy_quantity"}, schemaRequired(t, item))
		needed := schemaObject(t, schemaProperties(t, item)["needed"])
		assert.ElementsMatch(t, []string{"id", "name", "quantity", "aisle_number", "price"}, schemaRequired(t, needed))
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
	assert.Equal(t, "24 oz", got[0].Needed.Quantity)
	assert.Equal(t, "1 package", got[0].BuyQuantity)
	require.NotNil(t, got[0].Product)
	assert.Equal(t, "large", got[0].Product.ProductID)
	assert.Equal(t, "32 oz", got[0].Product.Size)
	assert.Equal(t, &price, got[0].Product.PriceRegular)
	require.NotNil(t, got[1].Product)
	assert.Empty(t, got[1].BuyQuantity)
	assert.Equal(t, "3 cloves", got[1].Needed.Quantity)
	assert.NotNil(t, catalog[0].Grade)
	assert.Equal(t, IngredientEmbedding{1, 2}, catalog[0].Embedding)
}

func TestMergeShoppingPurchaseResults(t *testing.T) {
	for _, tc := range []struct{ name, output, wantErr, quantity, product string }{
		{name: "multiple packages", output: `{"items":[{"needed":{"name":"Pork","quantity":"48 oz"},"product_id":"pork","buy_quantity":"2 packages"}]}`, quantity: "2 packages", product: "pork"},
		{name: "unmatched", output: `{"items":[{"needed":{"name":"Saffron","quantity":"1 pinch"},"product_id":"","buy_quantity":""}]}`},
		{name: "unknown product", output: `{"items":[{"needed":{"name":"Pork","quantity":"24 oz"},"product_id":"invented","buy_quantity":"1 package"}]}`, wantErr: "unknown product"},
		{name: "purchase without product", output: `{"items":[{"needed":{"name":"Pork","quantity":"24 oz"},"product_id":"","buy_quantity":"1 package"}]}`, wantErr: "no catalog product"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return shoppingMergeHTTPResponse(req, tc.output), nil })}, nil)
			got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{Name: "Pork", Quantity: "24 oz"}}, []InputIngredient{{ProductID: "pork", Description: "Pork", Size: "32 oz"}})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tc.quantity, got[0].BuyQuantity)
			if tc.product == "" {
				assert.Nil(t, got[0].Product)
				assert.Equal(t, "1 pinch", got[0].Needed.Quantity)
			} else {
				require.NotNil(t, got[0].Product)
				assert.Equal(t, tc.product, got[0].Product.ProductID)
			}
		})
	}
}

func TestMergeShoppingQuantitiesDecodeFailure(t *testing.T) {
	for _, output := range []string{`invalid`, `[]`, `{"items":[{"needed":{"quantity":3}}]}`, `{"items":"invalid"}`} {
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
