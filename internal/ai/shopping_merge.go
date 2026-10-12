package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type shoppingIngredientsResponse struct {
	Items []Ingredient `json:"items"`
}

// MatchingShoppingCatalog keeps only products referenced by recipe ingredient IDs.
func MatchingShoppingCatalog(ingredients []Ingredient, catalog []InputIngredient) []InputIngredient {
	ids := make(map[string]bool, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.ProductID != "" {
			ids[ingredient.ProductID] = true
		}
	}
	matched := make([]InputIngredient, 0)
	for _, product := range catalog {
		if ids[product.ProductID] {
			matched = append(matched, product)
		}
	}
	return matched
}

const shoppingPurchaseInstructions = `Merge the recipe ingredients into a shopping list using the supplied matching catalog.
Combine entries with the same nonempty product id, and equivalent pantry foods without ids. Keep different product ids and incompatible forms distinct. Convert compatible units only when unambiguous; preserve distinct amounts when conversion is uncertain. Return each item's original id and first name; pantry items use an empty id.
The output quantity is the amount to buy, not a separate recipe requirement. For matching catalog products, calculate enough packages from the combined recipe amount and package size. Use concise quantities such as "1 package (32 oz)", "2 packages (16 oz each)", or "1.5 lb" for products sold by weight. Never invent a product, size, or conversion. Never assume weight per clove, bunch, cup, or other ambiguous form.
For pantry items, missing catalog products, or uncertain package conversions, return the combined recipe amount as quantity. Usage notes such as "divided" do not add quantities. Remove or update approximate count annotations when merging changes their amount. Return every item in items.`

// MergeShoppingQuantities returns shopping amounts using a pre-matched catalog.
func (c *client) MergeShoppingQuantities(ctx context.Context, ingredients []Ingredient, catalog []InputIngredient) ([]Ingredient, error) {
	if len(ingredients) == 0 {
		return []Ingredient{}, nil
	}
	products := make([]InputIngredient, len(catalog))
	ids := make(map[string]bool, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.ProductID != "" {
			ids[ingredient.ProductID] = true
		}
	}
	for i, product := range catalog {
		product.Grade = nil
		product.Embedding = nil
		products[i] = product
	}
	input, err := json.Marshal(struct {
		Ingredients []Ingredient      `json:"ingredients"`
		Catalog     []InputIngredient `json:"catalog"`
	}{ingredients, products})
	if err != nil {
		return nil, fmt.Errorf("marshal shopping ingredients: %w", err)
	}
	text := scheme(c.shoppingSchema)
	text.Format.OfJSONSchema.Name = "shopping_ingredients"
	text.Format.OfJSONSchema.Strict = openai.Bool(true)
	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model:        gpt56Luna,
		Reasoning:    noReasoning(),
		ServiceTier:  c.serviceTier,
		Instructions: openai.String(shoppingPurchaseInstructions),
		Text:         text,
		Input:        responses.ResponseNewParamsInputUnion{OfInputItemList: []responses.ResponseInputItemUnionParam{user(string(input))}},
	})
	if err != nil {
		return nil, fmt.Errorf("merge shopping quantities: %w", err)
	}

	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryShoppingMerge, "model", gpt56Luna, responseUsageLogAttr(gpt56Luna, resp.Usage, string(resp.ServiceTier)))
	var merged shoppingIngredientsResponse
	if err := json.Unmarshal([]byte(resp.OutputText()), &merged); err != nil {
		return nil, fmt.Errorf("decode shopping ingredients: %w", err)
	}
	for _, item := range merged.Items {
		if item.ProductID != "" && !ids[item.ProductID] {
			return nil, fmt.Errorf("shopping merge returned unknown product %q", item.ProductID)
		}
	}
	return merged.Items, nil
}
