package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

const (
	defaultIngredientGradeModel = openai.ChatModelGPT5Mini
)

const ingredientGradeSystemInstruction = `
You review grocery catalog items before they are shown to a home recipe generator.

Score each item from 0 to 10 for usefulness as an ingredient in home-cooked recipes.

Strongly reward:
- raw, fresh, whole, or minimally processed produce, meat, seafood, dairy, grains, legumes, herbs, and spices
- ingredients that can support many recipe styles or cuisines. Reward diverse ingredients that are hard to make at home.
- less common but real cooking ingredients, including greens, roots, organ meats, bones, and seasonal produce

Strongly penalize:
- ready-to-eat foods, meal kits, bowls, snack trays, party trays, dips, gravies, mixes, and prepared sides
- items already cooked, heavily seasoned, sauced, breaded, cured, or packaged with dip/sauce
- formats intended mainly for snacking or immediate eating rather than cooking
- pre-cut fruit unless it is still broadly useful for cooking or baking


Scoring anchors:
- 9-10: excellent raw/fresh flexible cooking ingredient, e.g. whole vegetables, greens, roots, raw meats, fresh fruit useful in baking/cooking
- 7-8: strong ingredient but with some limitation, e.g. pre-seasoned sausage, niche produce, soup bones, cooked seafood
- 4-6: usable but narrow, processed, pre-cut, pre-mixed, or convenience-oriented
- 0-3: ready-to-eat snack/meal/kit/dip/sauce/condiment with little recipe flexibility

Important calibration:
- Do not downgrade an ingredient just because it is uncommon. Rutabaga, collard greens, artichokes, yuca, pears, soup bones, and chicken livers are valid cooking ingredients.
- Do downgrade items whose catalog wording implies they are mostly finished foods or snack formats.

Return JSON only. Preserve each input id/index exactly. Be concise.`

// this is wire compatible with kroger.Ingredient eventually it should replace it in what staples returns
type InputIngredient struct {
	ProductID    string           `json:"id,omitempty"`
	AisleNumber  string           `json:"number,omitempty"` // this is a dumb json name fix it later
	Brand        string           `json:"brand,omitempty"`
	Description  string           `json:"description,omitempty"`
	Size         string           `json:"size,omitempty"`
	PriceRegular *float32         `json:"regularPrice,omitempty"`
	PriceSale    *float32         `json:"salePrice,omitempty"`
	Categories   []string         `json:"categories,omitempty"`
	Grade        *IngredientGrade `json:"grade,omitempty"`
}

type IngredientGrade struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// Not a big fand of the number of places that normalize should be done once and not always
func NormalizeInputIngredient(ingredient InputIngredient) InputIngredient {
	ingredient.ProductID = strings.TrimSpace(ingredient.ProductID)
	ingredient.AisleNumber = strings.TrimSpace(ingredient.AisleNumber)
	ingredient.Brand = strings.TrimSpace(ingredient.Brand)
	ingredient.Description = strings.TrimSpace(ingredient.Description)
	ingredient.Size = strings.TrimSpace(ingredient.Size)
	return ingredient
}

func (i InputIngredient) Hash() string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.ProductID))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Brand))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Description))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Size))))
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

type ingredientGradeResponseItem struct {
	ProductID string `json:"id"`
	Score     int    `json:"score" jsonschema:"minimum=0,maximum=10"`
	Reason    string `json:"reason"`
}

type ingredientBatchGradeResponse struct {
	Grades []ingredientGradeResponseItem `json:"grades" jsonschema:"required"`
}

type ingredientGrader struct {
	apiKey       string
	model        string
	cacheVersion string
	schema       map[string]any
}

func ingredientGradeCacheVersion(model, systemInstruction string) string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, model))
	lo.Must(io.WriteString(fnv, systemInstruction))
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func NewIngredientGrader(apiKey, model string) *ingredientGrader {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultIngredientGradeModel
	}
	return &ingredientGrader{
		apiKey:       strings.TrimSpace(apiKey),
		model:        model,
		cacheVersion: ingredientGradeCacheVersion(model, ingredientGradeSystemInstruction),
		schema:       ingredientGradeJSONSchema(),
	}
}

func (g *ingredientGrader) CacheVersion() string {
	return g.cacheVersion
}

func (g *ingredientGrader) GradeIngredients(ctx context.Context, ingredients []InputIngredient) ([]InputIngredient, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	items := make([]InputIngredient, len(ingredients))
	for i, ingredient := range ingredients {
		item := NormalizeInputIngredient(ingredient)
		if item.Grade != nil {
			return nil, fmt.Errorf("already graded ingredient %s", item.ProductID)
		}
		items[i] = item
	}

	prompt, err := buildIngredientGradePrompt(items)
	if err != nil {
		return nil, fmt.Errorf("failed to build ingredient grading prompt: %w", err)
	}

	client := openai.NewClient(option.WithAPIKey(g.apiKey))
	resp, err := client.Responses.New(ctx, responses.ResponseNewParams{
		Model:        g.model,
		Instructions: openai.String(ingredientGradeSystemInstruction),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(prompt)},
		},
		Text: scheme(g.schema),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to grade ingredients: %w", err)
	}
	slog.InfoContext(ctx, "Ingredient grading usage", "model", g.model, responseUsageLogAttr(resp.Usage))

	return parseIngredientGrades(resp.OutputText(), items)
}

func buildIngredientGradePrompt(items []InputIngredient) (string, error) {
	type ingredientGradePromptItem struct {
		ProductID   string `json:"id"`
		Brand       string `json:"brand,omitempty"`
		Description string `json:"description,omitempty"`
		Size        string `json:"size,omitempty"`
	}
	promptItems := make([]ingredientGradePromptItem, len(items))
	for i, item := range items {
		promptItems[i] = ingredientGradePromptItem{
			ProductID:   item.ProductID,
			Brand:       item.Brand,
			Description: item.Description,
			Size:        item.Size,
		}
	}

	// TSV here instead?
	body, err := json.MarshalIndent(promptItems, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal ingredient batch: %w", err)
	}
	return fmt.Sprintf("Grade these grocery catalog items for home recipe generation.\nReturn one result per item, preserving each id exactly.\nReturn JSON only matching the provided schema.\nIngredient JSON:\n%s", string(body)), nil
}

func parseIngredientGrades(body string, items []InputIngredient) ([]InputIngredient, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty ingredient grading response from model")
	}

	itemMap := make(map[string]InputIngredient, len(items))
	for _, item := range items {
		productID := strings.TrimSpace(item.ProductID)
		if productID == "" {
			return nil, fmt.Errorf("ingredient product_id is required")
		}
		if _, ok := itemMap[productID]; ok {
			return nil, fmt.Errorf("ingredient grading duplicated input product_id %q", productID)
		}
		itemMap[productID] = item
	}

	var parsed ingredientBatchGradeResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ingredient grading response: %w", err)
	}
	if len(parsed.Grades) != len(items) {
		return nil, fmt.Errorf("ingredient grading response count mismatch: got %d want %d", len(parsed.Grades), len(items))
	}

	var graded []InputIngredient
	seen := make(map[string]bool, len(items))
	for _, result := range parsed.Grades {
		productID := strings.TrimSpace(result.ProductID)
		if productID == "" {
			return nil, fmt.Errorf("ingredient grade missing product_id")
		}
		item, ok := itemMap[productID]
		if !ok {
			return nil, fmt.Errorf("ingredient grade returned unknown product_id %q", productID)
		}
		if seen[productID] {
			return nil, fmt.Errorf("ingredient grading duplicated product_id %q", productID)
		}
		seen[productID] = true
		if result.Score < 0 || result.Score > 10 {
			return nil, fmt.Errorf("ingredient score must be between 0 and 10")
		}
		if strings.TrimSpace(result.Reason) == "" {
			return nil, fmt.Errorf("ingredient grading reason is required")
		}

		item.Grade = &IngredientGrade{
			Score:  result.Score,
			Reason: strings.TrimSpace(result.Reason),
		}
		graded = append(graded, item)
	}

	return graded, nil
}

func ingredientGradeJSONSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&ingredientBatchGradeResponse{})
	body, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("marshal ingredient grade schema: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("decode ingredient grade schema: %v", err))
	}
	return out
}
