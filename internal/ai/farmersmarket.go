package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/invopop/jsonschema"
)

const farmersMarketIngredientModel = "gpt-5"

type FarmersMarketPhoto struct {
	DataURL string
}

type farmersMarketIngredientItem struct {
	Name       string   `json:"name" jsonschema:"required"`
	Brand      string   `json:"brand" jsonschema:"required"`
	Size       string   `json:"size" jsonschema:"required"`
	Categories []string `json:"categories" jsonschema:"required"`
}

type farmersMarketIngredientResponse struct {
	Ingredients []farmersMarketIngredientItem `json:"ingredients" jsonschema:"required"`
}

const farmersMarketIngredientInstructions = `
Identify recipe-friendly ingredients visible in farmers market photos.

Return only foods or cooking ingredients that a home cook could buy and cook with today.
Include produce, herbs, eggs, meat, seafood, dairy, bread, grains, legumes, mushrooms, preserves, and similar market foods.
Skip non-food items, people, signs that are not tied to a food item, decorations, and duplicate views of the same item.

For each ingredient:
- name: plain ingredient name, such as "heirloom tomatoes", "rainier cherries", or "fresh eggs".
- brand: if a farm, stall, vendor, or store name is visible near that ingredient on a sign, label, tag, crate, tent, or package, use that visible name. If no clear source name is visible, use "Farmers market".
- size: include a visible package or unit only if useful, such as "1 pint", "per lb", "1 bunch", or "dozen"; otherwise use an empty string.
- categories: short grocery categories, such as "produce", "fruit", "vegetables", "herbs", "eggs", "meat", "seafood", "dairy", "bakery", or "pantry"; use an empty array if no category is clear.

Do not invent prices, brands, farms, sizes, or ingredients that are not visible. Return JSON only.`

func (c *client) ExtractFarmersMarketIngredients(ctx context.Context, photos []FarmersMarketPhoto) ([]InputIngredient, error) {
	if len(photos) == 0 {
		return nil, fmt.Errorf("at least one photo is required")
	}

	content := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentParamOfInputText("Extract the farmers market ingredients from these photos."),
	}
	for _, photo := range photos {
		dataURL := strings.TrimSpace(photo.DataURL)
		if dataURL == "" {
			continue
		}
		image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
		image.OfInputImage.ImageURL = openai.String(dataURL)
		content = append(content, image)
	}
	if len(content) == 1 {
		return nil, fmt.Errorf("at least one image is required")
	}

	params := responses.ResponseNewParams{
		Model:        farmersMarketIngredientModel,
		Instructions: openai.String(farmersMarketIngredientInstructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser),
			},
		},
		Store: openai.Bool(true),
		Text:  scheme(farmersMarketIngredientSchema()),
	}

	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("extract farmers market ingredients: %w", err)
	}
	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryFarmersMarket, "model", farmersMarketIngredientModel, responseUsageLogAttr(farmersMarketIngredientModel, resp.Usage))

	var parsed farmersMarketIngredientResponse
	if err := json.Unmarshal([]byte(resp.OutputText()), &parsed); err != nil {
		return nil, fmt.Errorf("parse farmers market ingredient response: %w", err)
	}

	ingredients := make([]InputIngredient, 0, len(parsed.Ingredients))
	for _, item := range parsed.Ingredients {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		brand := strings.TrimSpace(item.Brand)
		if brand == "" {
			brand = "Farmers market"
		}
		ingredient := NormalizeInputIngredient(InputIngredient{
			Brand:       brand,
			Description: name,
			Size:        strings.TrimSpace(item.Size),
			Categories:  normalizedCategories(item.Categories),
		})
		ingredient.ProductID = "farmersmarket_item_" + ingredient.Hash()
		ingredients = append(ingredients, ingredient)
	}
	return ingredients, nil
}

func farmersMarketIngredientSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&farmersMarketIngredientResponse{})
	raw, _ := json.Marshal(schema)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func normalizedCategories(categories []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		if category == "" {
			continue
		}
		if _, ok := seen[category]; ok {
			continue
		}
		seen[category] = struct{}{}
		out = append(out, category)
	}
	return out
}
