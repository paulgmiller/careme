package ai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeShoppingQuantitiesUsesLunaAndValidatesAllGroups(t *testing.T) {
	output := `{"items":[{"key":"product:garlic","quantity":"3 cloves"}]}`
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"model":"`+gpt56Luna+`"`)
		assert.Contains(t, string(body), `"reasoning":{"effort":"none"}`)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
			"id":"resp-merge","object":"response","created_at":1778529600,"status":"completed","model":%q,
			"output":[{"id":"msg-merge","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":%q,"annotations":[]}]}],
			"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}
		}`, gpt56Luna, output))), Request: req}, nil
	})}, nil)
	groups := []ShoppingQuantityGroup{{Key: "product:garlic", Name: "Garlic", Quantities: []string{"1 clove", "2 cloves"}}}
	got, err := client.MergeShoppingQuantities(t.Context(), groups)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"product:garlic": "3 cloves"}, got)
	_, err = client.MergeShoppingQuantities(t.Context(), append(groups, ShoppingQuantityGroup{Key: "name:salt", Name: "Salt", Quantities: []string{"1 tsp"}}))
	require.ErrorContains(t, err, "omitted groups")
}
