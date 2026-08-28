package recipeeval

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRecipeGenerator struct {
	recipe             *ai.Recipe
	recipeErr          error
	recipeInstructions []string
	menuRef            ai.ResponseRef
}

func (s *stubRecipeGenerator) GenerateRecipe(_ context.Context, instructions []string, menu ai.ResponseRef) (*ai.Recipe, error) {
	s.recipeInstructions = instructions
	s.menuRef = menu
	return s.recipe, s.recipeErr
}

const validRecipeContext = `{
	"vars": {
		"menu_plan": {
			"plans": [{
				"cuisine": "Italian",
				"anchor_ingredient": "Chicken Thighs",
				"technique": "sheet pan",
				"side_vegetable": "Broccoli",
				"recipe_instructions": ["Keep dinner quick"]
			}],
			"response_id": "resp-menu",
			"prompt_cache_key": "cache-key"
		}
	}
}`

func TestRunEvalGeneratesRecipeFromProvidedMenuPlan(t *testing.T) {
	generator := &stubRecipeGenerator{
		recipe: &ai.Recipe{
			Title:          "Sheet Pan Chicken",
			Description:    "A quick chicken dinner.",
			Instructions:   []string{"Heat the oven.", "Roast the chicken."},
			ResponseID:     "resp-recipe",
			PromptCacheKey: "cache-key",
		},
	}

	result, err := runEval([]byte(validRecipeContext), generator)
	require.NoError(t, err)

	output, ok := result["output"].(string)
	require.True(t, ok)
	assert.Contains(t, output, `"title":"Sheet Pan Chicken"`)
	assert.NotContains(t, output, "resp-recipe")
	assert.NotContains(t, output, "cache-key")
	assert.Equal(t, ai.ResponseRef{ID: "resp-menu", PromptCacheKey: "cache-key"}, generator.menuRef)
	assert.Equal(t, []string{
		"Cuisine direction for this recipe: Italian.",
		"Anchor ingredient direction for this recipe: Chicken Thighs.",
		"Suggested technique for this recipe: sheet pan.",
		"Side vegetable direction for this recipe: Broccoli.",
		"User direction for this recipe: Keep dinner quick",
	}, generator.recipeInstructions)
}

func TestRunEvalRejectsEmptyMenuPlan(t *testing.T) {
	ctx := []byte(`{"vars":{"menu_plan":{"response_id":"resp-menu"}}}`)

	result, err := runEval(ctx, &stubRecipeGenerator{})

	assert.Nil(t, result)
	require.EqualError(t, err, "eval menu plan must contain exactly one recipe plan")
}

func TestRunEvalRejectsMultipleRecipePlans(t *testing.T) {
	ctx := []byte(`{
		"vars": {
			"menu_plan": {
				"plans": [{}, {}],
				"response_id": "resp-menu"
			}
		}
	}`)

	result, err := runEval(ctx, &stubRecipeGenerator{})

	assert.Nil(t, result)
	require.EqualError(t, err, "eval menu plan must contain exactly one recipe plan")
}

func TestRunEvalRejectsMissingMenuResponseID(t *testing.T) {
	ctx := []byte(`{"vars":{"menu_plan":{"plans":[{}]}}}`)

	result, err := runEval(ctx, &stubRecipeGenerator{})

	assert.Nil(t, result)
	require.EqualError(t, err, "eval menu plan response id is required")
}

func TestRunEvalReturnsRecipeError(t *testing.T) {
	generator := &stubRecipeGenerator{recipeErr: errors.New("model unavailable")}

	result, err := runEval([]byte(validRecipeContext), generator)

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to generate recipe: model unavailable")
}

func TestRunEvalRejectsInvalidJSON(t *testing.T) {
	result, err := runEval([]byte(`{"vars":`), &stubRecipeGenerator{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, "failed to decode Promptfoo context")
}
