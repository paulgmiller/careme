package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// MergeShoppingQuantities reconciles recipe amounts without changing product identity.
// TODO: Match merged ingredients with InputIngredient catalog entries and use their
// package sizes to determine how many packages to buy.
func (c *client) MergeShoppingQuantities(ctx context.Context, ingredients []Ingredient) ([]Ingredient, error) {
	if len(ingredients) == 0 {
		return []Ingredient{}, nil
	}
	input, err := json.Marshal(ingredients)
	if err != nil {
		return nil, fmt.Errorf("marshal shopping ingredients: %w", err)
	}
	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:        gpt56Luna,
		Reasoning:    noReasoning(),
		ServiceTier:  c.serviceTier,
		Instructions: openai.String("Merge this JSON array of shopping ingredients into a JSON array of ingredients with combined quantities. Combine entries for the same product ID; when no ID is provided, combine entries for the same ingredient name. Keep different product IDs separate. Convert compatible units when unambiguous and keep distinct forms when conversion is uncertain. Preserve all ingredients and the first entry's id, name, aisle_number, and price for each merged ingredient. Never invent a product or package count. Return only the JSON array, with no markdown or other text."),
		Input:        responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{user(string(input))}},
	})
	if err != nil {
		return nil, fmt.Errorf("merge shopping quantities: %w", err)
	}

	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryShoppingMerge, "model", gpt56Luna, responseUsageLogAttr(gpt56Luna, resp.Usage, string(resp.ServiceTier)))
	var merged []Ingredient
	if err := json.Unmarshal([]byte(resp.OutputText()), &merged); err != nil {
		return nil, fmt.Errorf("decode shopping ingredients: %w", err)
	}
	return merged, nil
}
