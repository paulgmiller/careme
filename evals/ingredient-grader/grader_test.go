package grader

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubIngredientGrader struct {
	inputs []ai.InputIngredient
	grades []ai.InputIngredient
	err    error
}

func (s *stubIngredientGrader) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	s.inputs = append([]ai.InputIngredient(nil), ingredients...)
	return s.grades, s.err
}

func promptfooContextFromJSON(t *testing.T, value string) map[string]interface{} {
	t.Helper()
	var ctx map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(value), &ctx))
	return ctx
}

func TestRunEvalPassesScoresWithinInclusiveBounds(t *testing.T) {
	grader := &stubIngredientGrader{
		grades: []ai.InputIngredient{
			{
				ProductID:   "0",
				Description: "Broccoli Crowns",
				Grade:       &ai.IngredientGrade{Score: 8, Reason: "fresh vegetable"},
			},
			{
				ProductID:   "rice",
				Description: "Ready Rice",
				Grade:       &ai.IngredientGrade{Score: 6, Reason: "convenience food"},
			},
		},
	}
	ctx := promptfooContextFromJSON(t, `{
		"vars": {
			"cases": [
				{
					"ingredient": {"description": "Broccoli Crowns"},
					"expect": {"min": 8}
				},
				{
					"ingredient": {"id": "rice", "description": "Ready Rice"},
					"expect": {"max": 6}
				}
			]
		}
	}`)

	result, err := runEval(ctx, grader)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"output": "PASS"}, result)
	require.Len(t, grader.inputs, 2)
	assert.Equal(t, "0", grader.inputs[0].ProductID)
	assert.Equal(t, "rice", grader.inputs[1].ProductID)
}

func TestRunEvalReportsScoresOutsideBounds(t *testing.T) {
	grader := &stubIngredientGrader{
		grades: []ai.InputIngredient{
			{
				ProductID:   "low",
				Description: "Plain Lentils",
				Grade:       &ai.IngredientGrade{Score: 4, Reason: "scored too low"},
			},
			{
				ProductID:   "high",
				Description: "Prepared Dip",
				Grade:       &ai.IngredientGrade{Score: 8, Reason: "scored too high"},
			},
		},
	}
	ctx := promptfooContextFromJSON(t, `{
		"vars": {
			"cases": [
				{
					"ingredient": {"id": "low"},
					"expect": {"min": 5, "max": 7}
				},
				{
					"ingredient": {"id": "high"},
					"expect": {"min": 5, "max": 7}
				}
			]
		}
	}`)

	result, err := runEval(ctx, grader)
	require.NoError(t, err)
	output, ok := result["output"].(string)
	require.True(t, ok)
	assert.Contains(t, output, "grade=4<5 desc=Plain Lentils reason=scored too low")
	assert.Contains(t, output, "grade=8>7  desc=Prepared Dip reason=scored too high")
}

func TestRunEvalReturnsGraderError(t *testing.T) {
	grader := &stubIngredientGrader{err: errors.New("grader unavailable")}

	result, err := runEval(map[string]interface{}{}, grader)

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to grade ingredients: grader unavailable")
}

func TestRunEvalRejectsContextThatCannotBeEncoded(t *testing.T) {
	grader := &stubIngredientGrader{}
	ctx := map[string]interface{}{"unsupported": make(chan struct{})}

	result, err := runEval(ctx, grader)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
	assert.Contains(t, err.Error(), "chan struct")
	assert.Empty(t, grader.inputs)
}
