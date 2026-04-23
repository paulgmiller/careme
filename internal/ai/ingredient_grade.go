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

	"careme/internal/kroger"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/samber/lo"
)

const (
	defaultIngredientGradeModel = openai.ChatModelGPT5Nano
	ingredientGradeSchemaV1     = "ingredient-grade-v1"
)

const ingredientGradeSystemInstruction = `
You review grocery catalog items before they are shown to a home recipe generator.

Score each ingredient from 0 to 10 for how useful it is in a normal home-cooked recipe while considering:
- likely freshness and perishability from the catalog wording
- whether the item is a practical cooking ingredient instead of a novelty, supplement, or ready-to-eat product
- whether it gives the recipe model flexible cooking options

Scoring guidance:
- 9-10: excellent fresh or versatile cooking ingredient
- 7-8: strong ingredient with minor limitations
- 4-6: usable but limited, highly processed, or unclear
- 0-3: poor fit for home recipe generation

Return JSON only. Be concise.`

type IngredientSnapshot struct {
	ProductID    string   `json:"product_id,omitempty"`
	AisleNumber  string   `json:"aisle_number,omitempty"`
	Brand        string   `json:"brand,omitempty"`
	Description  string   `json:"description,omitempty"`
	Size         string   `json:"size,omitempty"`
	PriceRegular string   `json:"price_regular,omitempty"`
	PriceSale    string   `json:"price_sale,omitempty"`
	Categories   []string `json:"categories,omitempty"`
}

func SnapshotFromKrogerIngredient(ingredient kroger.Ingredient) IngredientSnapshot {
	return IngredientSnapshot{
		ProductID:    strings.TrimSpace(lo.FromPtr(ingredient.ProductId)),
		AisleNumber:  strings.TrimSpace(lo.FromPtr(ingredient.AisleNumber)),
		Brand:        strings.TrimSpace(lo.FromPtr(ingredient.Brand)),
		Description:  strings.TrimSpace(lo.FromPtr(ingredient.Description)),
		Size:         strings.TrimSpace(lo.FromPtr(ingredient.Size)),
		PriceRegular: priceToString(ingredient.PriceRegular),
		PriceSale:    priceToString(ingredient.PriceSale),
		Categories:   normalizeCategories(lo.FromPtr(ingredient.Categories)),
	}
}

// how are we using htis
func (s IngredientSnapshot) Hash() string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.ProductID))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Brand))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Description))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Size))))
	categories := append([]string(nil), s.Categories...)
	categories = normalizeCategories(categories)
	for _, category := range categories {
		lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(category))))
	}
	// should we embed model/prompt into this hash to force re greade on changes
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

type IngredientGrade struct {
	SchemaVersion string             `json:"schema_version"`
	Score         int                `json:"score"`
	Reason        string             `json:"reason"`
	Ingredient    IngredientSnapshot `json:"ingredient"`
	Model         string             `json:"model,omitempty" jsonschema:"-"`
	GradedAt      time.Time          `json:"graded_at,omitempty" jsonschema:"-"`
}

type ingredientBatchItem struct {
	IngredientID string             `json:"ingredient_id"`
	Ingredient   IngredientSnapshot `json:"ingredient"`
}

type ingredientGradeResponseItem struct {
	IngredientID string `json:"ingredient_id"`
	Score        int    `json:"score" jsonschema:"minimum=0,maximum=10"`
	Reason       string `json:"reason"`
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

func (g *ingredientGrader) Ready(ctx context.Context) error {
	client := openai.NewClient(option.WithAPIKey(g.apiKey))
	_, err := client.Models.List(ctx)
	return err
}

func (g *ingredientGrader) GradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]*IngredientGrade, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	snapshots := make([]IngredientSnapshot, len(ingredients))
	items := make([]ingredientBatchItem, len(ingredients))
	for i, ingredient := range ingredients {
		snapshot := SnapshotFromKrogerIngredient(ingredient)
		snapshots[i] = snapshot
		items[i] = ingredientBatchItem{
			IngredientID: batchIngredientID(snapshot, i),
			Ingredient:   snapshot,
		}
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

	grades, err := parseIngredientGrades(resp.OutputText(), items)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(resp.Model)
	if model == "" {
		model = g.model
	}
	gradedAt := time.Now().UTC()
	for _, grade := range grades {
		grade.Model = model
		grade.GradedAt = gradedAt
	}
	return grades, nil
}

func buildIngredientGradePrompt(items []ingredientBatchItem) (string, error) {
	body, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal ingredient batch: %w", err)
	}
	return fmt.Sprintf("Grade these grocery catalog items for home recipe generation.\nReturn one result per item, preserving each ingredient_id exactly.\nReturn JSON only matching the provided schema.\nIngredient JSON:\n%s", string(body)), nil
}

func parseIngredientGrades(body string, items []ingredientBatchItem) ([]*IngredientGrade, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty ingredient grading response from model")
	}
	var parsed ingredientBatchGradeResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ingredient grading response: %w", err)
	}

	if len(parsed.Grades) != len(items) {
		return nil, fmt.Errorf("ingredient grading response count mismatch: got %d want %d", len(parsed.Grades), len(items))
	}

	grades := make([]*IngredientGrade, len(items))
	itemIndexByID := make(map[string]int, len(items))
	for i, item := range items {
		if strings.TrimSpace(item.IngredientID) == "" {
			return nil, fmt.Errorf("ingredient batch item %d missing ingredient_id", i)
		}
		if _, ok := itemIndexByID[item.IngredientID]; ok {
			return nil, fmt.Errorf("ingredient batch duplicated ingredient_id %q", item.IngredientID)
		}
		itemIndexByID[item.IngredientID] = i
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range parsed.Grades {
		ingredientID := strings.TrimSpace(item.IngredientID)
		if ingredientID == "" {
			return nil, fmt.Errorf("ingredient grading ingredient_id is required")
		}
		index, ok := itemIndexByID[ingredientID]
		if !ok {
			return nil, fmt.Errorf("ingredient grading ingredient_id %q not found in request", ingredientID)
		}
		if _, ok := seen[ingredientID]; ok {
			return nil, fmt.Errorf("ingredient grading duplicated ingredient_id %q", ingredientID)
		}
		seen[ingredientID] = struct{}{}
		if item.Score < 0 || item.Score > 10 {
			return nil, fmt.Errorf("ingredient score must be between 0 and 10")
		}
		if strings.TrimSpace(item.Reason) == "" {
			return nil, fmt.Errorf("ingredient grading reason is required")
		}
		grades[index] = &IngredientGrade{
			SchemaVersion: ingredientGradeSchemaV1,
			Score:         item.Score,
			Reason:        strings.TrimSpace(item.Reason),
			Ingredient:    items[index].Ingredient,
		}
	}

	for i, grade := range grades {
		if grade == nil {
			return nil, fmt.Errorf("ingredient grading missing ingredient_id %q", items[i].IngredientID)
		}
	}
	return grades, nil
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

func batchIngredientID(snapshot IngredientSnapshot, index int) string {
	id := snapshot.Hash()
	if id == "" {
		return fmt.Sprintf("ingredient-%d", index)
	}
	return id
}
