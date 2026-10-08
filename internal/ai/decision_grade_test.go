package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decisionHTTPResponse(req *http.Request, answers string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-6-luna","answers":` + answers + `,"usage":{"input_tokens":10,"total_tokens":10}}`)),
		Request:    req,
	}
}

func TestDecisionGraderRequestAndScores(t *testing.T) {
	for _, tc := range []struct {
		score float64
		want  int
	}{{0, 1}, {6.49, 7}, {6.5, 8}, {9, 10}} {
		t.Run(fmt.Sprint(tc.score), func(t *testing.T) {
			grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "/v1/decisions", req.URL.Path)
				assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); !assert.NoError(t, err) {
					return nil, err
				}
				assert.Equal(t, gpt6Luna, body["model"])
				assert.NotContains(t, body, "reasoning")
				assert.NotContains(t, body, "text")
				var input []map[string]any
				if err := json.Unmarshal([]byte(body["input"].(string)), &input); !assert.NoError(t, err) {
					return nil, err
				}
				assert.Equal(t, []map[string]any{{"index": float64(0), "brand": "Farm", "description": "Asparagus", "size": "1 lb"}}, input)
				questions := body["questions"].([]any)
				assert.Len(t, questions, 1)
				question := questions[0].(map[string]any)
				assert.Equal(t, "score", question["type"])
				assert.Equal(t, "ingredient_0", question["name"])
				assert.Contains(t, question["instructions"], "Do not downgrade an ingredient just because it is uncommon")
				assert.NotContains(t, question["instructions"], "Return JSON")
				levels := question["levels"].([]any)
				assert.Len(t, levels, 10)
				assert.Equal(t, "1", levels[0].(map[string]any)["label"])
				assert.Equal(t, "10", levels[9].(map[string]any)["label"])
				return decisionHTTPResponse(req, fmt.Sprintf(`[{"type":"score","name":"ingredient_0","score":%f,"confidence":0.8,"probabilities":[]}]`, tc.score)), nil
			})})
			graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: " a ", Brand: " Farm ", Description: " Asparagus ", Size: " 1 lb ", PriceSale: new(float32(2)), Categories: []string{"Produce"}}})
			require.NoError(t, err)
			require.Len(t, graded, 1)
			assert.Equal(t, "a", graded[0].ProductID)
			assert.Equal(t, tc.want, graded[0].Grade.Score)
			assert.Contains(t, graded[0].Grade.Reason, ingredientGradeCriteria[tc.want-1])
			assert.Contains(t, graded[0].Grade.Reason, "confidence:0.800000")
			assert.Equal(t, float32(2), *graded[0].PriceSale)
			assert.Equal(t, []string{"Produce"}, graded[0].Categories)
		})
	}
}

func TestDecisionGraderRejectsFailedAnswers(t *testing.T) {
	for _, tc := range []struct{ name, answers, want string }{
		{"refusal", `[{"type":"refusal","name":"ingredient_0"}]`, "decision refused"},
		{"missing", `[]`, "expected 1 answers"},
		{"extra", `[{"type":"score","score":1},{"type":"score","score":2}]`, "expected 1 answers"},
		{"predicate", `[{"type":"predicate","probability":0.9}]`, "unexpected decision answer type"},
		{"low", `[{"type":"score","score":-1}]`, "score must be between 0 and 9"},
		{"high", `[{"type":"score","score":10}]`, "score must be between 0 and 9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return decisionHTTPResponse(req, tc.answers), nil
			})})
			graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "bad"}})
			require.ErrorContains(t, err, "grade ingredient")
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, graded)
		})
	}
}

func TestDecisionGraderBatchFailsCompletely(t *testing.T) {
	for _, tc := range []struct{ name, answers, want string }{
		{"refused item", `[{"type":"score","score":8,"confidence":1},{"type":"refusal","name":"ingredient_1"}]`, "grade ingredient bad: decision refused"},
		{"missing item", `[{"type":"score","score":8,"confidence":1}]`, "expected 2 answers"},
		{"transport error", "", "grade ingredient batch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if tc.answers == "" {
					return nil, fmt.Errorf("transport failed")
				}
				return decisionHTTPResponse(req, tc.answers), nil
			})})
			inputs := []InputIngredient{{ProductID: "good", Description: "good"}, {ProductID: "bad", Description: "bad"}}
			graded, err := grader.GradeIngredients(t.Context(), inputs)
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, graded)
			assert.Nil(t, inputs[0].Grade)
		})
	}
}

func TestDecisionGraderBatchesAllIngredientsInOneCall(t *testing.T) {
	inputs := make([]InputIngredient, 65)
	for i := range inputs {
		inputs[i] = InputIngredient{ProductID: fmt.Sprint(i), Description: fmt.Sprintf("Item %d", i)}
	}
	calls := 0
	grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body struct {
			Input     string                                `json:"input"`
			Questions []struct{ Name, Instructions string } `json:"questions"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		var states []struct {
			Index       int
			Description string
		}
		require.NoError(t, json.Unmarshal([]byte(body.Input), &states))
		require.Len(t, states, len(inputs))
		require.Len(t, body.Questions, len(inputs))
		answers := make([]string, len(inputs))
		for i, question := range body.Questions {
			assert.Equal(t, fmt.Sprintf("ingredient_%d", i), question.Name)
			assert.Contains(t, question.Instructions, fmt.Sprintf("item at index %d", i))
			assert.Equal(t, i, states[i].Index)
			assert.Equal(t, inputs[i].Description, states[i].Description)
			answers[i] = fmt.Sprintf(`{"type":"score","name":%q,"score":%d,"confidence":1}`, question.Name, i%10)
		}
		return decisionHTTPResponse(req, "["+strings.Join(answers, ",")+"]"), nil
	})})
	graded, err := grader.GradeIngredients(t.Context(), inputs)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	require.Len(t, graded, len(inputs))
	for i, item := range graded {
		assert.Equal(t, inputs[i].ProductID, item.ProductID)
		assert.Equal(t, i%10+1, item.Grade.Score)
	}
}

func TestDecisionGraderSkipsEmptyAndRejectsAlreadyGraded(t *testing.T) {
	var calls atomic.Int32
	grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return decisionHTTPResponse(req, `[]`), nil
	})})
	graded, err := grader.GradeIngredients(t.Context(), nil)
	require.NoError(t, err)
	assert.Nil(t, graded)
	_, err = grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "a"}, {ProductID: "b", Grade: &IngredientGrade{Score: 8}}})
	require.ErrorContains(t, err, "already graded ingredient b")
	assert.Zero(t, calls.Load())
}

func TestDecisionGradeCacheVersion(t *testing.T) {
	grader := NewDecisionGrader("test-key", http.DefaultClient)
	assert.Equal(t, IngredientGradeCacheVersion("decisions"), grader.CacheVersion())
	assert.NotEqual(t, IngredientGradeCacheVersion("gpt-6-luna"), grader.CacheVersion())
	before := grader.CacheVersion()
	original := ingredientGradeCriteria[0]
	t.Cleanup(func() { ingredientGradeCriteria[0] = original })
	ingredientGradeCriteria[0] = "changed rubric"
	assert.NotEqual(t, before, grader.CacheVersion())
}
