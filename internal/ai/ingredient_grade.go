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
	defaultIngredientGradeModel = openai.ChatModelGPT4_1Nano
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

type IngredientDecision string

const (
	IngredientDecisionKeep  IngredientDecision = "keep"
	IngredientDecisionMaybe IngredientDecision = "maybe"
	IngredientDecisionDrop  IngredientDecision = "drop"
)

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

func (s IngredientSnapshot) Hash() string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.ProductID))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.AisleNumber))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Brand))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Description))))
	lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(s.Size))))
	lo.Must(io.WriteString(fnv, strings.TrimSpace(s.PriceRegular)))
	lo.Must(io.WriteString(fnv, strings.TrimSpace(s.PriceSale)))
	categories := append([]string(nil), s.Categories...)
	categories = normalizeCategories(categories)
	for _, category := range categories {
		lo.Must(io.WriteString(fnv, strings.ToLower(strings.TrimSpace(category))))
	}
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

type IngredientGrade struct {
	SchemaVersion string             `json:"schema_version"`
	Score         int                `json:"score"`
	Decision      IngredientDecision `json:"decision"`
	Reason        string             `json:"reason"`
	Ingredient    IngredientSnapshot `json:"ingredient"`
	Model         string             `json:"model,omitempty" jsonschema:"-"`
	GradedAt      time.Time          `json:"graded_at,omitempty" jsonschema:"-"`
}

type ingredientGradeResponse struct {
	Score  int    `json:"score" jsonschema:"minimum=0,maximum=10"`
	Reason string `json:"reason"`
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

func (g *ingredientGrader) GradeIngredient(ctx context.Context, ingredient kroger.Ingredient) (*IngredientGrade, error) {
	snapshot := SnapshotFromKrogerIngredient(ingredient)
	prompt, err := buildIngredientGradePrompt(snapshot)
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
		return nil, fmt.Errorf("failed to grade ingredient: %w", err)
	}
	slog.InfoContext(ctx, "Ingredient grading usage", "model", g.model, responseUsageLogAttr(resp.Usage))

	grade, err := parseIngredientGrade(resp.OutputText(), snapshot)
	if err != nil {
		return nil, err
	}
	grade.Model = resp.Model
	if strings.TrimSpace(grade.Model) == "" {
		grade.Model = g.model
	}
	grade.GradedAt = time.Now().UTC()
	return grade, nil
}

func buildIngredientGradePrompt(snapshot IngredientSnapshot) (string, error) {
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal ingredient snapshot: %w", err)
	}
	return fmt.Sprintf("Grade this grocery catalog item for home recipe generation.\nReturn JSON only matching the provided schema.\nIngredient JSON:\n%s", string(body)), nil
}

func parseIngredientGrade(body string, snapshot IngredientSnapshot) (*IngredientGrade, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty ingredient grading response from model")
	}
	var parsed ingredientGradeResponse
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ingredient grading response: %w", err)
	}
	if parsed.Score < 0 || parsed.Score > 10 {
		return nil, fmt.Errorf("ingredient score must be between 0 and 10")
	}
	if strings.TrimSpace(parsed.Reason) == "" {
		return nil, fmt.Errorf("ingredient grading reason is required")
	}
	return &IngredientGrade{
		SchemaVersion: ingredientGradeSchemaV1,
		Score:         parsed.Score,
		Decision:      decisionFromScore(parsed.Score),
		Reason:        strings.TrimSpace(parsed.Reason),
		Ingredient:    snapshot,
	}, nil
}

func decisionFromScore(score int) IngredientDecision {
	switch {
	case score >= 7:
		return IngredientDecisionKeep
	case score >= 4:
		return IngredientDecisionMaybe
	default:
		return IngredientDecisionDrop
	}
}

func ingredientGradeJSONSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&ingredientGradeResponse{})
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
