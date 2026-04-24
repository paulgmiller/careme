package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

const (
	defaultIngredientGradeModel = openai.ChatModelGPT5Mini
	ingredientGradeSchemaV1     = "ingredient-grade-v1"
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
	SchemaVersion string    `json:"schema_version,omitempty"`
	Score         int       `json:"score"`
	Reason        string    `json:"reason"`
	Model         string    `json:"model,omitempty" jsonschema:"-"`
	GradedAt      time.Time `json:"graded_at,omitempty" jsonschema:"-"`
}

// Not a big fand of the number of places that normalize should be done once and not always
func NormalizeInputIngredient(ingredient InputIngredient) InputIngredient {
	ingredient.ProductID = strings.TrimSpace(ingredient.ProductID)
	ingredient.AisleNumber = strings.TrimSpace(ingredient.AisleNumber)
	ingredient.Brand = strings.TrimSpace(ingredient.Brand)
	ingredient.Description = strings.TrimSpace(ingredient.Description)
	ingredient.Size = strings.TrimSpace(ingredient.Size)
	ingredient.Categories = normalizeCategories(ingredient.Categories)
	return ingredient
}

func (i InputIngredient) Hash() string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.ProductID))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Brand))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Description))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(i.Size))))
	lo.Must(io.WriteString(fnv, ingredientGradeSystemInstruction))
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
	apiKey string
	model  string
	schema map[string]any
}

func NewIngredientGrader(apiKey, model string) *ingredientGrader {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultIngredientGradeModel
	}
	return &ingredientGrader{
		apiKey: strings.TrimSpace(apiKey),
		model:  model,
		schema: ingredientGradeJSONSchema(),
	}
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

	model := strings.TrimSpace(resp.Model)
	if model == "" {
		model = g.model
	}
	return parseIngredientGrades(resp.OutputText(), items, model, time.Now().UTC())
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

	body, err := json.MarshalIndent(promptItems, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal ingredient batch: %w", err)
	}
	return fmt.Sprintf("Grade these grocery catalog items for home recipe generation.\nReturn one result per item, preserving each id exactly.\nReturn JSON only matching the provided schema.\nIngredient JSON:\n%s", string(body)), nil
}

func parseIngredientGrades(body string, items []InputIngredient, model string, gradedAt time.Time) ([]InputIngredient, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty ingredient grading response from model")
	}

	itemIndexByProductID := make(map[string]int, len(items))
	for i, item := range items {
		productID := strings.TrimSpace(item.ProductID)
		if productID == "" {
			return nil, fmt.Errorf("ingredient product_id is required")
		}
		if _, ok := itemIndexByProductID[productID]; ok {
			return nil, fmt.Errorf("ingredient grading duplicated input product_id %q", productID)
		}
		itemIndexByProductID[productID] = i
	}

	var parsed ingredientBatchGradeResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ingredient grading response: %w", err)
	}
	if len(parsed.Grades) != len(items) {
		gradesjson := lo.Must(json.MarshalIndent(parsed.Grades, "", "  "))
		itemsjson := lo.Must(json.MarshalIndent(items, "", "  "))
		return nil, fmt.Errorf("ingredient grading response count mismatch: got %d want %d\nGrades:\n%s\nItems:\n%s", len(parsed.Grades), len(items), gradesjson, itemsjson)
	}

	graded := make([]InputIngredient, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, result := range parsed.Grades {
		productID := strings.TrimSpace(result.ProductID)
		if productID == "" {
			return nil, fmt.Errorf("ingredient grade missing product_id")
		}
		index, ok := itemIndexByProductID[productID]
		if !ok {
			return nil, fmt.Errorf("ingredient grade returned unknown product_id %q", productID)
		}
		if _, ok := seen[productID]; ok {
			return nil, fmt.Errorf("ingredient grading duplicated product_id %q", productID)
		}
		seen[productID] = struct{}{}
		if result.Score < 0 || result.Score > 10 {
			return nil, fmt.Errorf("ingredient score must be between 0 and 10")
		}
		if strings.TrimSpace(result.Reason) == "" {
			return nil, fmt.Errorf("ingredient grading reason is required")
		}

		item := items[index]
		item.Grade = &IngredientGrade{
			SchemaVersion: ingredientGradeSchemaV1,
			Score:         result.Score,
			Reason:        strings.TrimSpace(result.Reason),
			Model:         model,
			GradedAt:      gradedAt,
		}
		graded[index] = item
	}

	for i := range graded {
		if graded[i].Grade == nil {
			return nil, fmt.Errorf("ingredient grading missing product_id %q", items[i].ProductID)
		}
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

func normalizeCategories(categories []string) []string {
	if len(categories) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(categories))
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		key := strings.ToLower(category)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, category)
	}
	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out
}

func priceToString(price *float32) string {
	if price == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *price)
}
