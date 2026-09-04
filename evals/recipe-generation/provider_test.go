package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type stubCritiquer struct {
	received ai.Recipe
	err      error
	delay    time.Duration
}

func (s *stubCritiquer) CritiqueRecipe(_ context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	s.received = recipe
	time.Sleep(s.delay)
	return &ai.RecipeCritique{OverallScore: 8, Summary: "Good dinner.", SuggestedFixes: []string{}, Model: "fixed-judge"}, s.err
}

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

	result, err := runEval([]byte(validRecipeContext), generator, &stubCritiquer{})
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

	result, err := runEval(ctx, &stubRecipeGenerator{}, &stubCritiquer{})

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

	result, err := runEval(ctx, &stubRecipeGenerator{}, &stubCritiquer{})

	assert.Nil(t, result)
	require.EqualError(t, err, "eval menu plan must contain exactly one recipe plan")
}

func TestRunEvalRejectsMissingMenuResponseID(t *testing.T) {
	ctx := []byte(`{"vars":{"menu_plan":{"plans":[{}]}}}`)

	result, err := runEval(ctx, &stubRecipeGenerator{}, &stubCritiquer{})

	assert.Nil(t, result)
	require.EqualError(t, err, "eval menu plan response id is required")
}

func TestRunEvalReturnsRecipeError(t *testing.T) {
	generator := &stubRecipeGenerator{recipeErr: errors.New("model unavailable")}

	result, err := runEval([]byte(validRecipeContext), generator, &stubCritiquer{})

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to generate recipe: model unavailable")
}

func TestRunEvalRejectsInvalidJSON(t *testing.T) {
	result, err := runEval([]byte(`{"vars":`), &stubRecipeGenerator{}, &stubCritiquer{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, "failed to decode Promptfoo context")
}

func TestRunEvalJudgesGeneratedRecipeAndSeparatesLatency(t *testing.T) {
	generator := &stubRecipeGenerator{recipe: &ai.Recipe{Title: "Fresh output", ResponseID: "private", OriginHash: "origin", ParentHash: "parent"}}
	judge := &stubCritiquer{delay: 30 * time.Millisecond}
	result, err := runEval([]byte(validRecipeContext), generator, judge)
	require.NoError(t, err)
	assert.Equal(t, "Fresh output", judge.received.Title)
	assert.Empty(t, judge.received.ResponseID)
	assert.Empty(t, judge.received.OriginHash)
	assert.Empty(t, judge.received.ParentHash)
	metadata := result["metadata"].(map[string]interface{})
	assert.GreaterOrEqual(t, metadata["judgeLatencyMs"].(int64), int64(30))
	assert.Less(t, result["latencyMs"].(int64), metadata["judgeLatencyMs"].(int64))
	assert.Equal(t, 8, metadata["critique"].(*ai.RecipeCritique).OverallScore)
}

func TestRunEvalFailsWhenJudgeFails(t *testing.T) {
	generator := &stubRecipeGenerator{recipe: &ai.Recipe{Title: "Dinner"}}
	result, err := runEval([]byte(validRecipeContext), generator, &stubCritiquer{err: errors.New("unavailable")})
	assert.Nil(t, result)
	require.EqualError(t, err, "judge generated recipe: unavailable")
}

func TestDecodeOptionsModelSelection(t *testing.T) {
	t.Setenv("RECIPE_EVAL_MODEL", "environment-model")
	for _, tc := range []struct {
		name    string
		config  map[string]interface{}
		want    string
		wantErr bool
	}{
		{name: "environment", want: "environment-model"},
		{name: "explicit", config: map[string]interface{}{"model": " candidate-model ", "judge_model": "judge"}, want: "candidate-model"},
		{name: "invalid", config: map[string]interface{}{"model": 123}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := decodeOptions(map[string]interface{}{"config": tc.config})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, settings.Config.Model)
		})
	}
}

func TestCheckedInCasesGenerateAndJudge(t *testing.T) {
	body, err := os.ReadFile("promptfooconfig.yaml")
	require.NoError(t, err)
	var suite struct {
		Tests []struct {
			Description string                 `yaml:"description"`
			Vars        map[string]interface{} `yaml:"vars"`
		} `yaml:"tests"`
	}
	require.NoError(t, yaml.Unmarshal(body, &suite))
	require.Len(t, suite.Tests, 8)
	for _, tc := range suite.Tests {
		t.Run(tc.Description, func(t *testing.T) {
			body, err := json.Marshal(map[string]interface{}{"vars": tc.Vars})
			require.NoError(t, err)
			generator := &stubRecipeGenerator{recipe: &ai.Recipe{Title: "Generated"}}
			_, err = runEval(body, generator, &stubCritiquer{})
			require.NoError(t, err)
			assert.NotEmpty(t, generator.recipeInstructions)
		})
	}
}
