package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ShoppingItem keeps the recipe amount alongside an optional catalog purchase.
// A nil Product or empty BuyQuantity means the recommendation is unresolved.
type ShoppingItem struct {
	Needed      Ingredient       `json:"needed"`
	Product     *InputIngredient `json:"product"`
	BuyQuantity string           `json:"buy_quantity"`
}

type shoppingPurchaseResponse struct {
	Needed      Ingredient `json:"needed"`
	ProductID   string     `json:"product_id"`
	BuyQuantity string     `json:"buy_quantity"`
}

type shoppingIngredientsResponse struct {
	Items []shoppingPurchaseResponse `json:"items"`
}

const shoppingPurchaseInstructions = `Combine the recipe ingredient amounts and recommend purchases using only the supplied store catalog.
Merge equivalent ingredients without losing any recipe requirement. Keep different foods and incompatible forms distinct. Convert compatible units only when unambiguous; preserve distinct amounts when conversion is uncertain. Preserve the first recipe ingredient's id and name in needed.
Choose an equivalent catalog product, including a different package size when useful, and set product_id to its exact catalog id. Never invent a product, size, price, or conversion. Select enough food with the least excess; break comparable ties using total purchase price, using sale prices when available and regular prices otherwise. Do not replace ingredients with substantively different foods.
Set buy_quantity to a concise purchase amount such as "1 package", "2 packages", or "1.5 lb" for products sold by weight. Compute package counts from the combined needed amount and the catalog size. For example, if 24 oz is needed and the only matching package is 32 oz, buy 1 package. If sizes are 16 oz and 32 oz for a 24 oz need, both leave 8 oz excess; compare the price of two 16 oz packages with one 32 oz package. Never assume weight per clove, bunch, cup, or other ambiguous form.
If no catalog product matches, use an empty product_id and buy_quantity and retain the combined needed amount. If a product matches but its size or a conversion is uncertain, select it and leave buy_quantity empty. Use an empty id for needs without a recipe product id. Return every merged need in items.`

// MergeShoppingQuantities combines recipe needs and selects catalog purchases.
func (c *client) MergeShoppingQuantities(ctx context.Context, ingredients []Ingredient, catalog []InputIngredient) ([]ShoppingItem, error) {
	if len(ingredients) == 0 {
		return []ShoppingItem{}, nil
	}
	products := make([]InputIngredient, len(catalog))
	byID := make(map[string]InputIngredient, len(catalog))
	for i, product := range catalog {
		product.Grade = nil
		product.Embedding = nil
		products[i] = product
		if product.ProductID != "" {
			byID[product.ProductID] = product
		}
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
	items := make([]ShoppingItem, 0, len(merged.Items))
	for _, recommendation := range merged.Items {
		item := ShoppingItem{Needed: recommendation.Needed, BuyQuantity: recommendation.BuyQuantity}
		if recommendation.ProductID != "" {
			product, found := byID[recommendation.ProductID]
			if !found {
				return nil, fmt.Errorf("shopping purchase selected unknown product %q", recommendation.ProductID)
			}
			item.Product = &product
		} else if recommendation.BuyQuantity != "" {
			return nil, fmt.Errorf("shopping purchase quantity has no catalog product for %q", recommendation.Needed.Name)
		}
		items = append(items, item)
	}
	return items, nil
}
