package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"log/slog"

	"github.com/jmelahman/typesafe-sdk-go"
	"github.com/samber/lo"
)

type jevGrader struct {
	c *typesafe.Client
}

func NewJev() (*jevGrader, error) {
	client, err := typesafe.New() // WithAPIKey param
	if err != nil {
		log.Fatal(err)
	}
	return &jevGrader{c: client}, nil
}

type ingredientGradeState struct {
	Brand       string `json:"brand,omitempty"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
}

var ingredientGradeCriteria = typesafe.ScoreCriteria{
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

var ingredientGradeQuestion = typesafe.Score(
	ingredientGradeSystemInstruction,
	ingredientGradeCriteria,
)

func (g *jevGrader) CacheVersion() string {
	fnv := fnv.New128a()
	lo.Must(io.WriteString(fnv, ingredientGradeSystemInstruction))
	for _, crit := range ingredientGradeCriteria {
		lo.Must(io.WriteString(fnv, crit.(string)))
	}
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func (g *jevGrader) GradeIngredients(ctx context.Context, ingredients []InputIngredient) ([]InputIngredient, error) {
	if len(ingredients) == 0 {
		return nil, nil
	}

	var items []InputIngredient
	var requests []typesafe.SystemOneRequest
	for _, ingredient := range ingredients {
		item := NormalizeInputIngredient(ingredient)
		if item.Grade != nil {
			return nil, fmt.Errorf("already graded ingredient %s", item.ProductID)
		}
		items = append(items, item)
		req := typesafe.SystemOneRequest{
			State: ingredientGradeState{
				Brand:       item.Brand,
				Description: item.Description,
				Size:        item.Size,
			},
			Questions: typesafe.Questions{
				"ingredient_score": ingredientGradeQuestion,
			},
		}
		requests = append(requests, req)
	}

	slog.InfoContext(ctx, "got to call")

	results := g.c.SystemOneBatch(ctx, requests, 16)

	graded := make([]InputIngredient, 0, len(items))
	var inputTokens, outputTokens int
	for i, result := range results {
		if result.Err != nil {
			slog.ErrorContext(ctx, "oh no", "fail", result.Err.Error())
			return nil, fmt.Errorf(
				"grade ingredient %s: %w",
				items[i].ProductID,
				result.Err,
			)
		}

		inputTokens += result.Response.Usage.InputTokens
		outputTokens += result.Response.Usage.OutputTokens

		answer, err := result.Response.Score("ingredient_score")
		if err != nil {
			return nil, fmt.Errorf(
				"read ingredient score %s: %w",
				items[i].ProductID,
				err,
			)
		}
		// human grade low confidence?
		item := items[i]
		item.Grade = &IngredientGrade{
			Score:  answer.Level() + 1, // jev is 0-9 instead of 1-10
			Reason: fmt.Sprintf("confidence:%f, score:%f, level:%d", answer.Confidence, answer.Score, answer.Level()),
		}

		graded = append(graded, item)
	}
	slog.InfoContext(ctx, "Ingredient grading usage", "ai_category", aiCategoryIngredientGrading, "model", "jev", "input", inputTokens)

	return graded, nil
}
