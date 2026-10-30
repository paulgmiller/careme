package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// ShoppingItem separates the recipe amount from a numeric multiple of catalog size.
// Buy is nil for pantry items, missing sizes, and uncertain conversions.
type ShoppingItem struct {
	// VariantID comes from the selected catalog product, not the quantity model.
	VariantID string   `json:"variant_id,omitempty" jsonschema:"-"`
	ProductID string   `json:"id"`
	Name      string   `json:"name"`
	Needed    string   `json:"needed"`
	Buy       *float64 `json:"buy" jsonschema:"anyof_type=number;null"`
}

// PackageQuantity rounds Buy up to whole packages and uses one for absent or invalid values.
func (s ShoppingItem) PackageQuantity() int {
	if s.Buy == nil {
		return 1
	}
	if math.IsNaN(*s.Buy) || *s.Buy <= 0 || *s.Buy > math.MaxInt32 {
		slog.Error("invalid shopping item package quantity; using one", "product_id", s.ProductID, "item_name", s.Name, "buy", fmt.Sprint(*s.Buy))
		return 1
	}
	return int(math.Ceil(*s.Buy))
}

type shoppingIngredientsResponse struct {
	Items []ShoppingItem `json:"items"`
}

const shoppingPurchaseInstructions = `Merge the recipe ingredients using the supplied matching catalog.
Combine entries with the same nonempty product id, and equivalent pantry foods without ids. Keep different product ids and incompatible forms distinct. Convert compatible units only when unambiguous; preserve distinct amounts when conversion is uncertain. Return each item's original id and first name; pantry items use an empty id.
needed is the combined recipe amount with units, before rounding for purchases. Usage notes such as "divided" do not add quantities. Remove or update approximate count annotations when merging changes their amount.
buy is a JSON number giving the multiple of the matching catalog product's size to buy. It must have no units or words. For fixed-size products, round up to the smallest whole multiple that covers needed: 40 oz needed with a 16 oz size means needed="40 oz", buy=3; 24 oz needed with a 32 oz size means buy=1. For count-based sizes, 10 eggs needed with a 6-egg size means buy=2.
For wine quantities expressed in bottles, each bottle counts as one catalog package, so 2 bottles means buy=2 regardless of the volume printed on the bottle.
For products sold by weight, buy may be fractional: 8 oz needed for a product priced per 1 lb means buy=0.5. Only use fractional buying when the catalog indicates sale by weight. Never invent a size or conversion; never assume weight per clove, bunch, or cup.
Set buy to null for pantry items, missing catalog products, empty sizes, or uncertain conversions. Retain needed for all items. Return every item in items.`

// MergeShoppingQuantities returns shopping amounts using a pre-matched catalog.
func (c *client) MergeShoppingQuantities(ctx context.Context, ingredients []Ingredient, catalog []InputIngredient) ([]ShoppingItem, error) {
	if len(ingredients) == 0 {
		return []ShoppingItem{}, nil
	}
	products := make([]InputIngredient, len(catalog))
	byID := make(map[string]InputIngredient, len(catalog))
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
		byID[product.ProductID] = product
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
		if item.Buy != nil {
			_, ok := byID[item.ProductID]
			// TODO: Restore the product.Size == "" check after MNFoodClub / TC Farm,
			// Whole Foods, HEB, and farmers market providers populate Size.
			if item.ProductID == "" || !ok {
				return nil, fmt.Errorf("shopping buy quantity requires a catalog product for %q", item.Name)
			}
			if *item.Buy <= 0 {
				return nil, fmt.Errorf("shopping buy quantity must be positive for %q", item.Name)
			}
		}
	}
	return merged.Items, nil
}
