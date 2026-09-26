package eval

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/iotest"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validImageLatencyContext = `{
	"vars": {
		"image_style": "photo",
		"recipe": {
			"title": "Sheet Pan Chicken and Broccoli",
			"description": "Roasted chicken with crisp broccoli.",
			"instructions": ["Roast the chicken and broccoli until browned."]
		}
	}
}`

func TestRunEvalReportsImageStyle(t *testing.T) {
	for _, style := range []ai.RecipeImageStyle{ai.RecipeImagePhoto, ai.RecipeImageSketch} {
		t.Run(string(style), func(t *testing.T) {
			body := []byte(`{"vars":{"image_style":"` + string(style) + `","recipe":{"title":"Dinner","instructions":["Cook it."]}}}`)
			calls := 0
			measure := func(_ context.Context, _ imageGenerator, _ ai.Recipe, gotStyle ai.RecipeImageStyle) error {
				calls++
				assert.Equal(t, style, gotStyle)
				return nil
			}

			result, err := runEval(body, nil, measure)
			require.NoError(t, err)
			assert.Equal(t, 1, calls)
			assert.Equal(t, map[string]interface{}{"output": string(style)}, result)
		})
	}
}

func TestRunEvalRejectsIncompleteRecipe(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{"missing title", `{"vars":{"recipe":{"instructions":["Cook it."]}}}`, "eval recipe title is required"},
		{"missing instructions", `{"vars":{"recipe":{"title":"Dinner"}}}`, "at least one eval recipe instruction is required"},
		{"missing style", `{"vars":{"recipe":{"title":"Dinner","instructions":["Cook it."]}}}`, "eval image_style must be photo or sketch"},
		{"invalid style", `{"vars":{"image_style":"painting","recipe":{"title":"Dinner","instructions":["Cook it."]}}}`, "eval image_style must be photo or sketch"},
		{"invalid JSON", `{"vars":`, "failed to decode Promptfoo context"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := runEval([]byte(test.body), nil, nil)
			assert.Nil(t, result)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestRunEvalReturnsStyleSpecificError(t *testing.T) {
	measure := func(_ context.Context, _ imageGenerator, _ ai.Recipe, style ai.RecipeImageStyle) error {
		if style == ai.RecipeImagePhoto {
			return errors.New("model unavailable")
		}
		return nil
	}

	result, err := runEval([]byte(validImageLatencyContext), nil, measure)
	assert.Nil(t, result)
	require.EqualError(t, err, "measure photo latency: model unavailable")
}

type stubImageGenerator struct {
	image *ai.GeneratedImage
	err   error
}

func (s stubImageGenerator) GenerateRecipeImage(context.Context, ai.Recipe, ai.RecipeImageStyle) (*ai.GeneratedImage, error) {
	return s.image, s.err
}

func TestMeasureImageLatencyConsumesImage(t *testing.T) {
	err := measureImageLatency(t.Context(), stubImageGenerator{
		image: &ai.GeneratedImage{Body: bytes.NewBufferString("image")},
	}, ai.Recipe{Title: "Dinner"}, ai.RecipeImageSketch)
	require.NoError(t, err)

	err = measureImageLatency(t.Context(), stubImageGenerator{}, ai.Recipe{}, ai.RecipeImageSketch)
	require.EqualError(t, err, "image generation returned no image body")

	err = measureImageLatency(t.Context(), stubImageGenerator{
		image: &ai.GeneratedImage{Body: iotest.ErrReader(errors.New("read failed"))},
	}, ai.Recipe{}, ai.RecipeImageSketch)
	require.EqualError(t, err, "read generated image: read failed")
}
