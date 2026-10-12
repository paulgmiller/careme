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
The output quantity is the total amount to buy, expressed as a number with its unit, not a package count or a separate recipe requirement. For fixed-size catalog products, round up to the smallest whole multiple of the catalog size that covers the combined recipe amount and express the total in the catalog size's units. For example, 24 oz needed with a 32 oz size returns "32 oz"; 40 oz needed with a 16 oz size returns "48 oz". For count-based sizes use the total count, such as "12 eggs" for two 6-egg cartons. Do not include "package", "packages", "carton", or container counts in the output quantity.
For products sold by weight, return the weight to buy itself, such as "0.5 lb" for an 8 oz need, without rounding to a package size. Only use this rule when the catalog indicates the product is sold by weight. Never invent a product, size, or conversion. Never assume weight per clove, bunch, cup, or other ambiguous form.
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
