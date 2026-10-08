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
				var input map[string]any
				if err := json.Unmarshal([]byte(body["input"].(string)), &input); !assert.NoError(t, err) {
					return nil, err
				}
				assert.Equal(t, map[string]any{"brand": "Farm", "description": "Asparagus", "size": "1 lb"}, input)
				questions := body["questions"].([]any)
				assert.Len(t, questions, 1)
				question := questions[0].(map[string]any)
				assert.Equal(t, "score", question["type"])
				assert.Equal(t, "ingredient_score", question["name"])
				assert.Contains(t, question["instructions"], "Do not downgrade an ingredient just because it is uncommon")
				assert.NotContains(t, question["instructions"], "Return JSON")
				levels := question["levels"].([]any)
				assert.Len(t, levels, 10)
				assert.Equal(t, "1", levels[0].(map[string]any)["label"])
				assert.Equal(t, "10", levels[9].(map[string]any)["label"])
				return decisionHTTPResponse(req, fmt.Sprintf(`[{"type":"score","name":"ingredient_score","score":%f,"confidence":0.8,"probabilities":[]}]`, tc.score)), nil
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
		{"refusal", `[{"type":"refusal","name":"ingredient_score"}]`, "decision refused"},
		{"missing", `[]`, "expected one answer"},
		{"extra", `[{"type":"score","score":1},{"type":"score","score":2}]`, "expected one answer"},
		{"predicate", `[{"type":"predicate","probability":0.9}]`, "unexpected decision answer type"},
		{"low", `[{"type":"score","score":-1}]`, "score must be between 0 and 9"},
		{"high", `[{"type":"score","score":10}]`, "score must be between 0 and 9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return decisionHTTPResponse(req, tc.answers), nil
			})})
			graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "bad"}})
			require.ErrorContains(t, err, "grade ingredient bad")
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, graded)
		})
	}
}

func TestDecisionGraderBatchFailsCompletely(t *testing.T) {
	grader := NewDecisionGrader("test-key", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if strings.Contains(body.Input, "bad") {
			return nil, fmt.Errorf("transport failed")
		}
		return decisionHTTPResponse(req, `[{"type":"score","score":8,"confidence":1}]`), nil
	})})
	graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "good", Description: "good"}, {ProductID: "bad", Description: "bad"}})
	require.ErrorContains(t, err, "grade ingredient bad")
	assert.Nil(t, graded)
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
