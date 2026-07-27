package ai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFarmersMarketIngredientsUsesVisiblePrice(t *testing.T) {
	client := NewClient("test-key", "ignored", farmersMarketResponseClient(t), nil)

	got, err := client.ExtractFarmersMarketIngredients(t.Context(), "data:image/jpeg;base64,abc")

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "heirloom tomatoes", got[0].Description)
	assert.Equal(t, "River Farm", got[0].AisleNumber)
	assert.Equal(t, "River Farm", got[0].Brand)
	assert.Empty(t, got[0].Size)
	require.NotNil(t, got[0].PriceRegular)
	assert.InDelta(t, 4.99, *got[0].PriceRegular, 0.001)
	assert.Empty(t, got[0].Categories)
	assert.Nil(t, got[1].PriceRegular)
	assert.Equal(t, got[0].Description, got[1].Description)
	assert.NotEqual(t, got[0].Brand, got[1].Brand)
	assert.NotEqual(t, got[0].ProductID, got[1].ProductID)
}

func TestFarmersMarketIngredientSchemaUsesPriceNotSizeOrCategories(t *testing.T) {
	schema := farmersMarketIngredientSchema()
	properties := schemaProperties(t, schema)
	ingredients := schemaObject(t, properties["ingredients"])
	items := schemaObject(t, ingredients["items"])
	itemProperties := schemaProperties(t, items)

	assert.Contains(t, itemProperties, "price")
	assert.NotContains(t, itemProperties, "size")
	assert.NotContains(t, itemProperties, "categories")
	assert.Contains(t, schemaRequired(t, items), "price")
	assertSchemaAllowsNull(t, schemaObject(t, itemProperties["price"]))
	assertNoOneOf(t, schema)
}

func assertSchemaAllowsNull(t *testing.T, schema map[string]any) {
	t.Helper()
	types, ok := schema["type"].([]any)
	require.True(t, ok, "expected type union schema, got %#v", schema)
	assert.Contains(t, types, "number")
	assert.Contains(t, types, "null")
}

func assertNoOneOf(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		assert.NotContains(t, typed, "oneOf")
		for _, child := range typed {
			assertNoOneOf(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoOneOf(t, child)
		}
	}
}

func farmersMarketResponseClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/responses") {
			t.Fatalf("unexpected OpenAI request path: %s", req.URL.Path)
		}
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "price")
		assert.Contains(t, string(body), `"model":"`+gpt56Terra+`"`)
		assert.NotContains(t, string(body), `"size"`)
		assert.NotContains(t, string(body), `"categories"`)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id": "resp-farmers-market",
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [{
					"id": "msg-farmers-market",
					"type": "message",
					"status": "completed",
					"role": "assistant",
					"content": [{
						"type": "output_text",
						"text": "{\"ingredients\":[{\"name\":\"heirloom tomatoes\",\"brand\":\"River Farm\",\"price\":4.99},{\"name\":\"heirloom tomatoes\",\"brand\":\"Hill Farm\",\"price\":null}]}",
						"annotations": []
					}]
				}],
				"usage": {
					"input_tokens": 1,
					"input_tokens_details": {"cached_tokens": 0},
					"output_tokens": 1,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 2
				}
			}`, farmersMarketIngredientModel))),
			Request: req,
		}, nil
	})}
}
