package menuplaneval

import (
	"context"
	"errors"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMenuPlanner struct {
	location     *locations.Location
	ingredients  []ai.InputIngredient
	instructions []string
	date         time.Time
	lastRecipes  []string
	count        int
	plan         *ai.MenuPlan
	err          error
}

func (s *stubMenuPlanner) CreateMenuPlan(_ context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
	s.location = location
	s.ingredients = ingredients
	s.instructions = instructions
	s.date = date
	s.lastRecipes = lastRecipes
	s.count = count
	return s.plan, s.err
}

func TestRunEvalReturnsMenuPlanJSON(t *testing.T) {
	planner := &stubMenuPlanner{plan: &ai.MenuPlan{
		Plans: []ai.RecipePlan{{
			Cuisine:          "Italian",
			AnchorIngredient: "Chicken Thighs",
			Technique:        "sheet pan",
			SideVegetable:    "Broccoli",
		}},
		ChefNoteSuggestion: "faster dinners",
		ResponseID:         "resp-menu",
		PromptCacheKey:     "cache-key",
	}}
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1", "state": "WA"},
			"ingredients": [{"id": "chicken", "description": "Chicken Thighs"}],
			"instructions": "Keep dinner quick",
			"date": "2026-08-21",
			"last_recipes": ["Chicken soup"]
		}
	}`)

	result, err := runEval(ctx, planner)
	require.NoError(t, err)

	output, ok := result["output"].(string)
	require.True(t, ok)
	assert.JSONEq(t, `{
		"plans":[{"cuisine":"Italian","anchor_ingredient":"Chicken Thighs","technique":"sheet pan","side_vegetable":"Broccoli","fancy":false,"recipe_instructions":null}],
		"chef_note_suggestion":"faster dinners"
	}`, output)
	assert.Equal(t, "store-1", planner.location.ID)
	assert.Equal(t, []string{"Keep dinner quick"}, planner.instructions)
	assert.Equal(t, time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC), planner.date)
	assert.Equal(t, []string{"Chicken soup"}, planner.lastRecipes)
	assert.Equal(t, 1, planner.count)
	assert.NotContains(t, output, "resp-menu")
	assert.NotContains(t, output, "cache-key")
}

func TestRunEvalRejectsInvalidDate(t *testing.T) {
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1"},
			"ingredients": [{"id": "chicken"}],
			"date": "August 21"
		}
	}`)

	result, err := runEval(ctx, &stubMenuPlanner{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, `invalid eval date "August 21"`)
}

func TestRunEvalReturnsPlannerError(t *testing.T) {
	planner := &stubMenuPlanner{err: errors.New("model unavailable")}
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1"},
			"ingredients": [{"id": "chicken"}],
			"date": "2026-08-21"
		}
	}`)

	result, err := runEval(ctx, planner)

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to create menu plan: model unavailable")
}

func TestRunEvalRejectsInvalidJSON(t *testing.T) {
	result, err := runEval([]byte(`{"vars":`), &stubMenuPlanner{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, "failed to decode Promptfoo context")
}
