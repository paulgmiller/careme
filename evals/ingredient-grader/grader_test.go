package eval

import (
	"context"
	"encoding/json"
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

func TestCallApiReturnsConfigurationError(t *testing.T) {
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("INGREDIENT_GRADING_ENABLE", "1")
	t.Setenv("INGREDIENT_GRADING_MODEL", "gpt-6-luna")
	t.Setenv("AI_API_KEY", "")

	result, err := CallApi("", nil, map[string]interface{}{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, "failed to load configuration: AI_API_KEY is required for ingredient grading")
}

func TestCallApiRejectsDisabledGrading(t *testing.T) {
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("INGREDIENT_GRADING_ENABLE", "false")
	t.Setenv("AI_API_KEY", "")

	result, err := CallApi("", nil, map[string]interface{}{})

	assert.Nil(t, result)
	require.EqualError(t, err, "ingredient grading eval requires INGREDIENT_GRADING_ENABLE to be enabled")
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
	assert.Equal(t, "PASS", result["output"])
	metadata := result["metadata"].(map[string]interface{})
	assert.Equal(t, 2, metadata["ingredientCount"])
	assert.Equal(t, 2, metadata["passedIngredientCount"])
	assert.Equal(t, grader.grades, metadata["grades"])
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
	assert.Equal(t, 0, result["metadata"].(map[string]interface{})["passedIngredientCount"])
}

func TestRunEvalReturnsGraderError(t *testing.T) {
	grader := &stubIngredientGrader{err: errors.New("grader unavailable")}

	ctx := promptfooContextFromJSON(t, `{"vars":{"cases":[{"ingredient":{"description":"Broccoli"}}]}}`)
	result, err := runEval(ctx, grader)

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to grade ingredients: grader unavailable")
}

func TestRunEvalRejectsIncompleteOrInvalidGrades(t *testing.T) {
	for _, tc := range []struct {
		name, wantErr string
		grades        []ai.InputIngredient
	}{
		{"missing", "received 0 of 1 grades", nil},
		{"nil grade", "missing grade", []ai.InputIngredient{{ProductID: "0"}}},
		{"unknown id", "unexpected or duplicate", []ai.InputIngredient{{ProductID: "other"}}},
		{"duplicate", "unexpected or duplicate", []ai.InputIngredient{
			{ProductID: "0", Grade: &ai.IngredientGrade{Score: 8}},
			{ProductID: "0", Grade: &ai.IngredientGrade{Score: 8}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := promptfooContextFromJSON(t, `{"vars":{"cases":[{"ingredient":{"description":"Broccoli"}}]}}`)
			result, err := runEval(ctx, &stubIngredientGrader{grades: tc.grades})
			require.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, result)
		})
	}
}

func TestRunEvalRejectsEmptyOrDuplicateCases(t *testing.T) {
	for _, ctxJSON := range []string{
		`{"vars":{"cases":[]}}`,
		`{"vars":{"cases":[{"ingredient":{"id":"same"}},{"ingredient":{"id":"same"}}]}}`,
	} {
		grader := &stubIngredientGrader{}
		_, err := runEval(promptfooContextFromJSON(t, ctxJSON), grader)
		require.Error(t, err)
		assert.Empty(t, grader.inputs)
	}
}

func TestRunEvalRejectsContextThatCannotBeEncoded(t *testing.T) {
	grader := &stubIngredientGrader{}
	ctx := map[string]interface{}{"unsupported": make(chan struct{})}

	result, err := runEval(ctx, grader)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type")
	assert.Empty(t, grader.inputs)
}
