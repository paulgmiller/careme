package eval

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validImageLatencyContext = `{
	"vars": {
		"recipe": {
			"title": "Sheet Pan Chicken and Broccoli",
			"description": "Roasted chicken with crisp broccoli.",
			"instructions": ["Roast the chicken and broccoli until browned."]
		}
	}
}`

func TestRunEvalComparesSketchAndPhotoLatency(t *testing.T) {
	var styles []ai.RecipeImageStyle
	measure := func(_ context.Context, _ imageGenerator, _ ai.Recipe, style ai.RecipeImageStyle) (time.Duration, error) {
		styles = append(styles, style)
		if style == ai.RecipeImageSketch {
			return 4 * time.Second, nil
		}
		return 10 * time.Second, nil
	}

	result, err := runEval([]byte(validImageLatencyContext), nil, imageModels{
		Sketch: "gpt-image-2.5-flare",
		Photo:  "gpt-image-2.5-sunburst",
	}, measure)
	require.NoError(t, err)

	assert.Equal(t, []ai.RecipeImageStyle{ai.RecipeImageSketch, ai.RecipeImagePhoto}, styles)
	assert.Equal(t, int64(14_000), result["latencyMs"])
	assert.JSONEq(t, `{
		"sketch":{"style":"sketch","model":"gpt-image-2.5-flare","latency_ms":4000},
		"photo":{"style":"photo","model":"gpt-image-2.5-sunburst","latency_ms":10000},
		"difference_ms":6000,
		"photo_to_sketch_ratio":2.5
	}`, result["output"].(string))
	metadata := result["metadata"].(map[string]interface{})
	assert.Equal(t, int64(4_000), metadata["sketchLatencyMs"])
	assert.Equal(t, int64(10_000), metadata["photoLatencyMs"])
}

func TestRunEvalRejectsIncompleteRecipe(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{"missing title", `{"vars":{"recipe":{"instructions":["Cook it."]}}}`, "eval recipe title is required"},
		{"missing instructions", `{"vars":{"recipe":{"title":"Dinner"}}}`, "at least one eval recipe instruction is required"},
		{"invalid JSON", `{"vars":`, "failed to decode Promptfoo context"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := runEval([]byte(test.body), nil, imageModels{}, nil)
			assert.Nil(t, result)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestRunEvalReturnsStyleSpecificError(t *testing.T) {
	measure := func(_ context.Context, _ imageGenerator, _ ai.Recipe, style ai.RecipeImageStyle) (time.Duration, error) {
		if style == ai.RecipeImagePhoto {
			return 0, errors.New("model unavailable")
		}
		return time.Second, nil
	}

	result, err := runEval([]byte(validImageLatencyContext), nil, imageModels{}, measure)
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
	result, err := measureImageLatency(t.Context(), stubImageGenerator{
		image: &ai.GeneratedImage{Body: bytes.NewBufferString("image")},
	}, ai.Recipe{Title: "Dinner"}, ai.RecipeImageSketch)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result, time.Duration(0))

	_, err = measureImageLatency(t.Context(), stubImageGenerator{}, ai.Recipe{}, ai.RecipeImageSketch)
	require.EqualError(t, err, "image generation returned no image body")
}
