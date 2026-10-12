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

func TestMergeShoppingQuantitiesSendsIngredientsAndReturnsModelJSON(t *testing.T) {
	ingredients := []Ingredient{
		{ProductID: "garlic", Name: "Garlic", Quantity: "1 clove", AisleNumber: "Produce", Price: "$2"},
		{ProductID: "garlic", Name: "Fresh Garlic", Quantity: "2 cloves", AisleNumber: "2", Price: "$3"},
		{ProductID: "other-garlic", Name: "Garlic", Quantity: "1 bulb"},
		{Name: "Salt", Quantity: "1 tsp"},
		{Name: "salt", Quantity: "2 tsp"},
	}
	want := []Ingredient{
		{Name: "Salt", Quantity: "3 tsp"},
		{ProductID: "other-garlic", Name: "Garlic", Quantity: "1 bulb"},
		{ProductID: "garlic", Name: "Garlic", Quantity: "3 cloves", AisleNumber: "Produce", Price: "$2"},
	}
	output, err := json.Marshal(want)
	require.NoError(t, err)
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model     string `json:"model"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Input []struct {
				Content string `json:"content"`
			} `json:"input"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, gpt56Luna, body.Model)
		assert.Equal(t, "none", body.Reasoning.Effort)
		require.Len(t, body.Input, 1)
		var sent []Ingredient
		require.NoError(t, json.Unmarshal([]byte(body.Input[0].Content), &sent))
		assert.Equal(t, ingredients, sent, "send every original ingredient without grouping")
		return shoppingMergeHTTPResponse(req, string(output)), nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), ingredients)
	require.NoError(t, err)
	assert.Equal(t, want, got, "return the model's ingredients in its chosen order")
	assert.Equal(t, "1 clove", ingredients[0].Quantity)
}

func TestMergeShoppingQuantitiesDecodeFailure(t *testing.T) {
	for _, output := range []string{`invalid`, `{"items":[]}`, `[{"quantity":3}]`} {
		t.Run(output, func(t *testing.T) {
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return shoppingMergeHTTPResponse(req, output), nil
			})}, nil)
			got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{Name: "Garlic", Quantity: "1 clove"}})
			require.ErrorContains(t, err, "decode shopping ingredients")
			assert.Nil(t, got)
		})
	}
}

func TestMergeShoppingQuantitiesAPIFailure(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid request","type":"invalid_request_error"}}`)), Request: req}, nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{{Name: "Garlic", Quantity: "1 clove"}})
	require.ErrorContains(t, err, "merge shopping quantities")
	assert.Nil(t, got)
}

func TestMergeShoppingQuantitiesEmptyInput(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("empty ingredients should not call AI")
		return nil, nil
	})}, nil)
	got, err := client.MergeShoppingQuantities(t.Context(), []Ingredient{})
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
