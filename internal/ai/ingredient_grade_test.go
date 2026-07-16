package ai

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInputIngredientNormalizesFieldsAndSetsID(t *testing.T) {
	ingredient := NormalizeInputIngredient(InputIngredient{
		ProductID:    " 123 ",
		AisleNumber:  " A5 ",
		Brand:        " Farm Stand ",
		Description:  "  Baby Spinach  ",
		Size:         " 5 oz ",
		PriceRegular: new(float32(4.99)),
		PriceSale:    new(float32(3.49)),
		Categories:   []string{" greens ", "Produce", "produce", ""},
	})

	assert.Equal(t, "123", ingredient.ProductID)
	assert.Equal(t, "A5", ingredient.AisleNumber)
	assert.Equal(t, "Farm Stand", ingredient.Brand)
	assert.Equal(t, "Baby Spinach", ingredient.Description)
	assert.Equal(t, "5 oz", ingredient.Size)
	require.NotNil(t, ingredient.PriceRegular)
	require.NotNil(t, ingredient.PriceSale)
	assert.Equal(t, float32(4.99), *ingredient.PriceRegular)
	assert.Equal(t, float32(3.49), *ingredient.PriceSale)
}

func TestInputIngredientHashStableAcrossCategoryOrder(t *testing.T) {
	left := NormalizeInputIngredient(InputIngredient{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Produce", "Greens"},
	})
	right := NormalizeInputIngredient(InputIngredient{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Greens", "Produce"},
	})

	assert.Equal(t, left.Hash(), right.Hash())
}

func TestIngredientGradeCacheVersionChangesWhenPromptOrModelChanges(t *testing.T) {
	base := (&ingredientGrader{cacheVersion: ingredientGradeCacheVersion(gpt56Luna, "prompt a")}).CacheVersion()
	same := (&ingredientGrader{cacheVersion: ingredientGradeCacheVersion(gpt56Luna, "prompt a")}).CacheVersion()
	differentModel := (&ingredientGrader{cacheVersion: ingredientGradeCacheVersion("gpt-5-nano", "prompt a")}).CacheVersion()
	differentPrompt := (&ingredientGrader{cacheVersion: ingredientGradeCacheVersion(gpt56Luna, "prompt b")}).CacheVersion()

	assert.Equal(t, base, same)
	assert.NotEqual(t, base, differentModel)
	assert.NotEqual(t, base, differentPrompt)
}

func TestBuildIngredientGradePrompt(t *testing.T) {
	ingredient := NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "foobar",
		Categories:  []string{"Produce"},
	})
	prompt, err := buildIngredientGradePrompt([]InputIngredient{ingredient})
	require.NoError(t, err)
	assert.Contains(t, prompt, "preserving each id")
	assert.Contains(t, prompt, `"id": "foobar"`)
	assert.Contains(t, prompt, "Return JSON only matching the provided schema.")
	assert.Contains(t, prompt, `"description": "Asparagus"`)
}

func TestGradeIngredientsUsesLunaWithoutReasoning(t *testing.T) {
	grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"model":"`+gpt56Luna+`"`)
		assert.Contains(t, string(body), `"reasoning":{"effort":"none"}`)

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id": "resp-grade",
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [{
					"id": "msg-grade",
					"type": "message",
					"status": "completed",
					"role": "assistant",
					"content": [{
						"type": "output_text",
						"text": "{\"grades\":[{\"id\":\"ingredient-1\",\"score\":8,\"reason\":\"Fresh vegetable.\"}]}",
						"annotations": []
					}]
				}],
				"usage": {
					"input_tokens": 1,
					"input_tokens_details": {"cached_tokens": 0},
					"output_tokens": 1,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 2
				}
			}`, gpt56Luna))),
			Request: req,
		}, nil
	})})

	graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "ingredient-1", Description: "Asparagus"}})

	require.NoError(t, err)
	require.Len(t, graded, 1)
	require.NotNil(t, graded[0].Grade)
	assert.Equal(t, 8, graded[0].Grade.Score)
}

func TestGradeIngredientsRejectsExtraProductIDsWithoutRetry(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	calls := 0
	grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return ingredientGradeHTTPResponse(req, `{"grades":[{"id":"wrong-1","score":8,"reason":"Fresh vegetable."}]}`), nil
	})})

	_, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "ingredient-1", Description: "Asparagus"}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown product_id "wrong-1"`)
	assert.Equal(t, 1, calls)
	assert.Contains(t, logs.String(), `msg="ingredient grade returned extra product"`)
	assert.Contains(t, logs.String(), `product_id=wrong-1`)
}

func TestGradeIngredientsLeavesMissingProductsUngraded(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	body := `{"grades":[{"id":"ingredient-1","score":8,"reason":"Fresh vegetable."}]}`
	calls := 0
	grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return ingredientGradeHTTPResponse(req, body), nil
	})})

	graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{
		{ProductID: "ingredient-1", Description: "Asparagus"},
		{ProductID: "ingredient-2", Description: "Broccoli"},
	})

	require.NoError(t, err)
	require.Len(t, graded, 2)
	assert.Equal(t, "ingredient-1", graded[0].ProductID)
	require.NotNil(t, graded[0].Grade)
	assert.Equal(t, "ingredient-2", graded[1].ProductID)
	assert.Nil(t, graded[1].Grade)
	assert.Equal(t, 1, calls)
	assert.Contains(t, logs.String(), `msg="ingredient grading response missing products"`)
	assert.Contains(t, logs.String(), `ingredient-2`)
}

func ingredientGradeHTTPResponse(req *http.Request, output string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
			"id":"resp-grade",
			"object":"response",
			"created_at":1778529600,
			"status":"completed",
			"model":%q,
			"output":[{"id":"msg-grade","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":%q,"annotations":[]}]}],
			"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}
		}`, gpt56Luna, output))),
		Request: req,
	}
}

func TestParseIngredientGrades(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "ingredient-1",
	})}
	graded, err := parseIngredientGrades(t.Context(), `{"grades":[{"id":"`+items[0].ProductID+`","score":8,"reason":"Fresh produce with broad weeknight use."}]}`, items)
	require.NoError(t, err)
	require.Len(t, graded, 1)
	require.NotNil(t, graded[0].Grade)
	assert.Equal(t, 8, graded[0].Grade.Score)
	assert.Equal(t, "Fresh produce with broad weeknight use.", graded[0].Grade.Reason)
	assert.Equal(t, "Asparagus", graded[0].Description)
}

func TestParseIngredientGradesRejectsInvalidResponses(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{ProductID: "ingredient-1"})}
	_, err := parseIngredientGrades(t.Context(), `{"grades":[{"id":"`+items[0].ProductID+`","score":11,"reason":"too high"}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 10")

	_, err = parseIngredientGrades(t.Context(), `{"grades":[{"id":"`+items[0].ProductID+`","score":3,"reason":"   "}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")

	graded, err := parseIngredientGrades(t.Context(), `{"grades":[]}`, items)
	require.NoError(t, err)
	require.Len(t, graded, 1)
	assert.Nil(t, graded[0].Grade)
}

func TestParseIngredientGradesMatchesByIDInsteadOfOrder(t *testing.T) {
	items := []InputIngredient{
		NormalizeInputIngredient(InputIngredient{Description: "Potato Chips", ProductID: "b"}),
		NormalizeInputIngredient(InputIngredient{Description: "Asparagus", ProductID: "a"}),
	}

	body := `{"grades":[{"id":"` + items[1].ProductID + `","score":9,"reason":"Fresh vegetable."},{"id":"` + items[0].ProductID + `","score":2,"reason":"Snack food."}]}`
	graded, err := parseIngredientGrades(t.Context(), body, items)
	require.NoError(t, err)
	require.Len(t, graded, 2)

	byID := make(map[string]InputIngredient, len(graded))
	for _, ingredient := range graded {
		require.NotNil(t, ingredient.Grade)
		byID[ingredient.ProductID] = ingredient
	}

	require.Contains(t, byID, "b")
	assert.Equal(t, "Potato Chips", byID["b"].Description)
	assert.Equal(t, 2, byID["b"].Grade.Score)

	require.Contains(t, byID, "a")
	assert.Equal(t, "Asparagus", byID["a"].Description)
	assert.Equal(t, 9, byID["a"].Grade.Score)
}

func TestParseIngredientGradesRejectsDuplicateInputProductIDs(t *testing.T) {
	items := []InputIngredient{
		NormalizeInputIngredient(InputIngredient{Description: "Asparagus", ProductID: "ingredient-1"}),
		NormalizeInputIngredient(InputIngredient{Description: "Broccoli", ProductID: "ingredient-1"}),
	}

	_, err := parseIngredientGrades(t.Context(), `{"grades":[{"id":"ingredient-1","score":8,"reason":"Fresh vegetable."},{"id":"ingredient-1","score":7,"reason":"Another vegetable."}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicated input product_id")
}

func TestIngredientGradeSchemaOmitsOperationalFields(t *testing.T) {
	schema := ingredientGradeJSONSchema()
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	_, hasSchemaVersion := properties["schema_version"]

	assert.False(t, hasSchemaVersion)
}
