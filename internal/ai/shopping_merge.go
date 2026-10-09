package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ShoppingQuantityGroup contains all recipe amounts for one store product.
type ShoppingQuantityGroup struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Quantities []string `json:"quantities"`
}

type shoppingQuantityResult struct {
	Key      string `json:"key"`
	Quantity string `json:"quantity"`
}

// MergeShoppingQuantities reconciles recipe amounts without changing product identity.
func (c *client) MergeShoppingQuantities(ctx context.Context, groups []ShoppingQuantityGroup) (map[string]string, error) {
	if len(groups) == 0 {
		return map[string]string{}, nil
	}
	input, err := json.Marshal(groups)
	if err != nil {
		return nil, fmt.Errorf("marshal shopping quantities: %w", err)
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"items": map[string]any{
			"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"key": map[string]any{"type": "string"}, "quantity": map[string]any{"type": "string"}},
				"required":   []string{"key", "quantity"},
			},
		}},
		"required": []string{"items"},
	}
	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:        gpt56Luna,
		Reasoning:    noReasoning(),
		ServiceTier:  c.serviceTier,
		Instructions: openai.String("Combine the quantities for each shopping product into one concise, useful shopping amount. Convert compatible units when unambiguous. Keep distinct forms when conversion is uncertain. Preserve every key exactly and return one item per input group. Never invent a product or package count."),
		Input:        responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{user(string(input))}},
		Text:         scheme(schema),
	})
	if err != nil {
		return nil, fmt.Errorf("merge shopping quantities: %w", err)
	}
	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryShoppingMerge, "model", gpt56Luna, responseUsageLogAttr(gpt56Luna, resp.Usage, string(resp.ServiceTier)))
	var parsed struct {
		Items []shoppingQuantityResult `json:"items"`
	}
	if err := json.Unmarshal([]byte(resp.OutputText()), &parsed); err != nil {
		return nil, fmt.Errorf("decode shopping quantities: %w", err)
	}
	want := make(map[string]bool, len(groups))
	for _, group := range groups {
		want[group.Key] = true
	}
	got := make(map[string]string, len(groups))
	for _, item := range parsed.Items {
		if !want[item.Key] || got[item.Key] != "" || strings.TrimSpace(item.Quantity) == "" {
			return nil, fmt.Errorf("invalid shopping quantity result for %q", item.Key)
		}
		got[item.Key] = strings.TrimSpace(item.Quantity)
	}
	if len(got) != len(want) {
		return nil, fmt.Errorf("shopping quantity result omitted groups")
	}
	return got, nil
}
