package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const DecisionsIngredientGrader = "decisions"

const ingredientDecisionInstruction = "Review only the grocery catalog item at index %d in the input list. Ignore all other items when assigning its score. Use the rubric below.\n" + ingredientGradeRubric

type decisionGrader struct {
	oai openai.Client
}

type ingredientGradeState struct {
	Index       int    `json:"index"`
	Brand       string `json:"brand,omitempty"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
}

var ingredientGradeCriteria = []string{
	// 1
	"Not meaningfully useful as a cooking ingredient; essentially a finished food, snack, dip, condiment, or meal.",
	// 2
	"Very limited cooking usefulness; heavily prepared, sauced, seasoned, breaded, or snack-oriented.",
	// 3
	"Some possible cooking use, but primarily a convenience food, mix, prepared side, or finished product.",
	// 4
	"Usable as an ingredient, but narrow or substantially processed.",
	// 5
	"Reasonably useful cooking ingredient with meaningful limitations in flexibility or processing.",
	// 6
	"Solid general cooking ingredient; useful in multiple dishes but not especially versatile or high quality.",
	// 7
	"Strong cooking ingredient with good flexibility; may have a minor limitation such as seasoning or niche use.",
	// 8
	"Very strong cooking ingredient: versatile, minimally processed, or notably good quality.",
	// 9
	"Excellent raw, fresh, minimally processed, flexible cooking ingredient or exceptionally good specialty ingredient.",
	// 10
	"Exceptional home-cooking ingredient: highly versatile, minimally processed, and broadly useful across recipes.",
}

func ingredientDecisionCacheVersion() string {
	rubric, err := json.Marshal(ingredientGradeCriteria)
	if err != nil {
		panic(err)
	}
	return ingredientGradeCacheVersion("decisions/batch-v2/"+gpt6Luna, ingredientDecisionInstruction+string(rubric)+"/round-score-plus-one")
}

func NewDecisionGrader(apiKey string, httpClient *http.Client) *decisionGrader {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	return &decisionGrader{oai: openai.NewClient(opts...)}
}

func (g *decisionGrader) CacheVersion() string { return ingredientDecisionCacheVersion() }

func (g *decisionGrader) GradeIngredients(ctx context.Context, ingredients []InputIngredient) ([]InputIngredient, error) {
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
	states := make([]ingredientGradeState, len(items))
	for i, item := range items {
		states[i] = ingredientGradeState{Index: i, Brand: item.Brand, Description: item.Description, Size: item.Size}
	}
	input, err := json.Marshal(states)
	if err != nil {
		return nil, fmt.Errorf("marshal ingredient grading batch: %w", err)
	}
	levels := make([]openai.DecisionNewParamsQuestionScoreLevel, len(ingredientGradeCriteria))
	for i, description := range ingredientGradeCriteria {
		levels[i] = openai.DecisionNewParamsQuestionScoreLevel{Label: fmt.Sprint(i + 1), Description: openai.String(description)}
	}
	questions := make([]openai.DecisionNewParamsQuestionUnion, len(items))
	for i := range items {
		questions[i] = openai.DecisionNewParamsQuestionUnion{OfScore: &openai.DecisionNewParamsQuestionScore{
			Name:         openai.String(fmt.Sprintf("ingredient_%d", i)),
			Instructions: fmt.Sprintf(ingredientDecisionInstruction, i),
			Levels:       levels,
		}}
	}
	decision, err := g.oai.Decisions.New(ctx, openai.DecisionNewParams{
		Model:     gpt6Luna,
		Input:     openai.DecisionNewParamsInputUnion{OfString: openai.String(string(input))},
		Questions: questions,
	})
	if err != nil {
		return nil, fmt.Errorf("grade ingredient batch: %w", err)
	}
	slog.InfoContext(ctx, "Ingredient grading usage", "ai_category", aiCategoryIngredientGrading, "model", gpt6Luna, "api", "decisions", "input", decision.Usage.InputTokens)
	if len(decision.Answers) != len(items) {
		return nil, fmt.Errorf("grade ingredient batch: expected %d answers, got %d", len(items), len(decision.Answers))
	}
	// Decisions returns answers in question order.
	for i, answer := range decision.Answers {
		grade, err := ingredientGradeFromDecision(answer)
		if err != nil {
			return nil, fmt.Errorf("grade ingredient %s: %w", items[i].ProductID, err)
		}
		items[i].Grade = grade
	}
	return items, nil
}

func ingredientGradeFromDecision(answer openai.DecisionAnswerUnion) (*IngredientGrade, error) {
	switch score := answer.AsAny().(type) {
	case openai.DecisionAnswerScore:
		if math.IsNaN(score.Score) || math.IsInf(score.Score, 0) || score.Score < 0 || score.Score > float64(len(ingredientGradeCriteria)-1) {
			return nil, fmt.Errorf("score must be between 0 and 9")
		}
		level := int(math.Round(score.Score))
		return &IngredientGrade{Score: level + 1, Reason: fmt.Sprintf("%s confidence:%f, score:%f, level:%d", ingredientGradeCriteria[level], score.Confidence, score.Score, level)}, nil
	case openai.DecisionAnswerRefusal:
		return nil, fmt.Errorf("decision refused")
	default:
		return nil, fmt.Errorf("unexpected decision answer type %q", answer.Type)
	}
}
