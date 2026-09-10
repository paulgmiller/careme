package eval

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCallJudgeUsesGPT56SolAndReturnsPromptfooGrade(t *testing.T) {
	t.Setenv("AI_API_KEY", "test-key")

	var requestBody string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		requestBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id":"resp-judge",
				"object":"response",
				"created_at":1788144000,
				"status":"completed",
				"model":%q,
				"output":[{"id":"msg-judge","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"pass\":true,\"score\":0.85,\"reason\":\"Useful and accurate.\"}","annotations":[]}]}],
				"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":20,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":120}
			}`, judgeModel))),
			Request: req,
		}, nil
	})}

	result, err := callJudge(t.Context(), "grade this critique", client)
	require.NoError(t, err)
	assert.JSONEq(t, `{"pass":true,"score":0.85,"reason":"Useful and accurate."}`, result["output"].(string))
	assert.Equal(t, map[string]interface{}{
		"prompt":     int64(100),
		"completion": int64(20),
		"total":      int64(120),
	}, result["tokenUsage"])
	assert.Contains(t, requestBody, `"model":"gpt-5.6-sol"`)
	assert.Contains(t, requestBody, "grade this critique")
	assert.Contains(t, requestBody, `"effort":"medium"`)
}

func TestCallApiReturnsReadableMissingKeyError(t *testing.T) {
	t.Setenv("AI_API_KEY", "")

	result, err := CallApi("grade", nil, nil)
	require.NoError(t, err)
	assert.Contains(t, result["error"], "AI_API_KEY is required")
}

func TestPromptWithRecipeAddsJSONFromPromptfooContext(t *testing.T) {
	prompt := promptWithRecipe("grade", map[string]interface{}{
		"vars": map[string]interface{}{
			"recipe": map[string]interface{}{"title": "Dinner"},
		},
	})

	assert.Contains(t, prompt, `<AuthoritativeRecipeJSON>`)
	assert.Contains(t, prompt, `{"title":"Dinner"}`)
	assert.Equal(t, "grade", promptWithRecipe("grade", nil))
}

func TestUsefulnessGradeSchemaRequiresAllFields(t *testing.T) {
	schema := usefulnessGradeSchema()

	assert.Equal(t, []string{"pass", "score", "reason"}, schema["required"])
	assert.Equal(t, false, schema["additionalProperties"])
}
