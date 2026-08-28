package critiqueeval

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubLoader struct {
	recipe *ai.Recipe
	err    error
	hash   string
}

func (s *stubLoader) SingleFromCache(_ context.Context, hash string) (*ai.Recipe, error) {
	s.hash = hash
	return s.recipe, s.err
}

type stubCritiquer struct {
	critique *ai.RecipeCritique
	err      error
	recipe   ai.Recipe
}

func (s *stubCritiquer) CritiqueRecipe(_ context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	s.recipe = recipe
	return s.critique, s.err
}

func TestRunEvalLoadsRecipeHashAndReturnsCritique(t *testing.T) {
	loader := &stubLoader{recipe: &ai.Recipe{Title: "Hash supper"}}
	critiquer := &stubCritiquer{critique: &ai.RecipeCritique{
		SchemaVersion: "recipe-critique-v1",
		OverallScore:  8,
		Summary:       "A useful recipe.",
	}}

	result, err := runEval(t.Context(), evalCase{RecipeHash: "recipe-hash"}, loader, critiquer)
	require.NoError(t, err)

	assert.Equal(t, "recipe-hash", loader.hash)
	assert.Equal(t, "Hash supper", critiquer.recipe.Title)
	assert.JSONEq(t, `{"schema_version":"recipe-critique-v1","overall_score":8,"summary":"A useful recipe.","strengths":null,"issues":null,"suggested_fixes":null,"critiqued_at":"0001-01-01T00:00:00Z"}`, result["output"].(string))
}

func TestRunEvalUsesInlineRecipeWithoutLoader(t *testing.T) {
	recipe := &ai.Recipe{Title: "Inline supper"}
	critiquer := &stubCritiquer{critique: &ai.RecipeCritique{OverallScore: 7, Summary: "Needs seasoning."}}

	result, err := runEval(t.Context(), evalCase{Recipe: recipe}, nil, critiquer)
	require.NoError(t, err)

	assert.NotNil(t, result)
	assert.Equal(t, "Inline supper", critiquer.recipe.Title)
}

func TestDecodeEvalCaseRequiresExactlyOneRecipeSource(t *testing.T) {
	for _, body := range []string{
		`{"vars":{}}`,
		`{"vars":{"recipe":{},"recipe_hash":"hash"}}`,
	} {
		_, err := decodeEvalCase([]byte(body))
		require.EqualError(t, err, "eval must provide exactly one of recipe or recipe_hash")
	}
}

func TestRunEvalReturnsLoadError(t *testing.T) {
	result, err := runEval(t.Context(), evalCase{RecipeHash: "missing"}, &stubLoader{err: errors.New("not found")}, &stubCritiquer{})

	assert.Nil(t, result)
	require.EqualError(t, err, "load recipe missing: not found")
}

func TestCritiqueRecipeReturnsModelError(t *testing.T) {
	result, err := critiqueRecipe(t.Context(), ai.Recipe{Title: "Supper"}, &stubCritiquer{err: errors.New("unavailable")})

	assert.Nil(t, result)
	require.EqualError(t, err, `critique recipe "Supper": unavailable`)
}
