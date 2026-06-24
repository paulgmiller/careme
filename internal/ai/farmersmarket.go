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

type farmersMarketIngredientItem struct {
	Name  string   `json:"name" jsonschema:"required"`
	Brand string   `json:"brand" jsonschema:"required"`
	Price *float32 `json:"price" jsonschema:"required"`
}

type farmersMarketIngredientResponse struct {
	Ingredients []farmersMarketIngredientItem `json:"ingredients" jsonschema:"required"`
}

const farmersMarketIngredientInstructions = `
Identify recipe-friendly ingredients visible in a farmers market photo.

Return only foods or cooking ingredients that a home cook could buy and cook with today.
Include produce, herbs, eggs, meat, seafood, dairy, bread, grains, legumes, mushrooms, preserves, and similar market foods.
Skip non-food items, people, signs that are not tied to a food item, decorations, and duplicate views of the same item.

For each ingredient:
- name: plain ingredient name, such as "heirloom tomatoes", "rainier cherries", or "fresh eggs".
- brand: if a farm, stall, vendor, or store name is visible near that ingredient on a sign, label, tag, crate, tent, or package, use that visible name. If no clear source name is visible, use "Farmers market".
- price: if signage visibly ties a price to that ingredient, return the best-effort numeric dollar price, such as 4.99 for "$4.99/lb" or 6 for "$6 per basket"; otherwise use null.

Do not invent prices, brands, farms, or ingredients that are not visible. Return JSON only.`

// url here can be public remote or data: url that bin64 encodes the image
// https://developers.openai.com/api/docs/guides/images-vision?format=base64-encoded
func (c *client) ExtractFarmersMarketIngredients(ctx context.Context, imageDataURL string) ([]InputIngredient, error) {
	imageDataURL = strings.TrimSpace(imageDataURL)
	if imageDataURL == "" {
		return nil, fmt.Errorf("image data URL is required")
	}

	content := responses.ResponseInputMessageContentListParam{
		responses.ResponseInputContentParamOfInputText("Extract the farmers market ingredients from this photo."),
	}
	image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
	image.OfInputImage.ImageURL = openai.String(imageDataURL)
	content = append(content, image)

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
		ingredient := NormalizeInputIngredient(InputIngredient{
			AisleNumber:  strings.TrimSpace(item.Brand),
			Description:  name,
			PriceRegular: item.Price,
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
