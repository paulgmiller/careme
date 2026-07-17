package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"careme/internal/ai"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturePlanner struct {
	plan         *ai.MenuPlan
	err          error
	location     *locationtypes.Location
	ingredients  []ai.InputIngredient
	instructions []string
	date         time.Time
	count        int
}

func (c *capturePlanner) CreateMenuPlan(
	_ context.Context,
	location *locationtypes.Location,
	ingredients []ai.InputIngredient,
	instructions []string,
	date time.Time,
	_ []string,
	count int,
) (*ai.MenuPlan, error) {
	c.location = location
	c.ingredients = ingredients
	c.instructions = instructions
	c.date = date
	c.count = count
	return c.plan, c.err
}

type captureGenerator struct {
	recipe       *ai.Recipe
	err          error
	instructions []string
	responseID   string
}

func (c *captureGenerator) GenerateRecipe(_ context.Context, instructions []string, responseID string) (*ai.Recipe, error) {
	c.instructions = instructions
	c.responseID = responseID
	return c.recipe, c.err
}

func TestRunGeneratesOneRecipeFromScenario(t *testing.T) {
	t.Parallel()

	planner := &capturePlanner{plan: &ai.MenuPlan{
		ResponseID: "menu-response",
		Plans: []ai.RecipePlan{{
			Cuisine:          "Mediterranean",
			AnchorIngredient: "Chicken Thighs",
			Technique:        "marinate and roast",
			SideVegetable:    "Broccoli",
		}},
	}}
	generator := &captureGenerator{recipe: &ai.Recipe{
		Title: "Lemon Chicken",
		Ingredients: []ai.Ingredient{
			{Name: "Chicken thighs", Quantity: "1 pound"},
		},
		Instructions: []string{"Roast 1 pound chicken thighs."},
	}}
	var out bytes.Buffer

	err := run(t.Context(), testScenarioJSON(), &out, planner, generator)

	require.NoError(t, err)
	assert.Equal(t, "WA", planner.location.State)
	require.Len(t, planner.ingredients, 2)
	assert.Equal(t, "chicken-1", planner.ingredients[0].ProductID)
	assert.Equal(t, []string{"Create a dinner that divides the oil between a marinade and roasting.", "Keep it practical."}, planner.instructions)
	assert.Equal(t, time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC), planner.date)
	assert.Equal(t, 1, planner.count)
	assert.Equal(t, "menu-response", generator.responseID)
	assert.Contains(t, generator.instructions, "Create a dinner that divides the oil between a marinade and roasting.")
	assert.Contains(t, generator.instructions, "Anchor ingredient direction for this recipe: Chicken Thighs.")

	var got ai.Recipe
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "Lemon Chicken", got.Title)
}

func TestRunReturnsHelpfulUpstreamErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		planner   *capturePlanner
		generator *captureGenerator
		want      string
	}{
		{
			name:      "menu planning",
			planner:   &capturePlanner{err: errors.New("planner unavailable")},
			generator: &captureGenerator{},
			want:      `create menu plan for "split marinade": planner unavailable`,
		},
		{
			name:      "empty plan",
			planner:   &capturePlanner{plan: &ai.MenuPlan{}},
			generator: &captureGenerator{},
			want:      `create menu plan for "split marinade": no plans returned`,
		},
		{
			name: "missing menu response id",
			planner: &capturePlanner{plan: &ai.MenuPlan{
				Plans: []ai.RecipePlan{{Cuisine: "Italian"}},
			}},
			generator: &captureGenerator{},
			want:      `create menu plan for "split marinade": response ID is required`,
		},
		{
			name: "recipe generation",
			planner: &capturePlanner{plan: &ai.MenuPlan{
				ResponseID: "menu-response",
				Plans:      []ai.RecipePlan{{Cuisine: "Italian"}},
			}},
			generator: &captureGenerator{err: errors.New("generator unavailable")},
			want:      `generate recipe for "split marinade": generator unavailable`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := run(t.Context(), testScenarioJSON(), &out, tt.planner, tt.generator)
			require.EqualError(t, err, tt.want)
			assert.Empty(t, out.String())
		})
	}
}

func TestParseScenarioValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "invalid json", raw: `{`, want: "decode scenario:"},
		{name: "name", raw: `{"location":{"state":"WA"},"date":"2026-07-16","ingredients":[{"description":"oil"}],"directive":"cook"}`, want: "scenario name is required"},
		{name: "state", raw: `{"name":"test","date":"2026-07-16","ingredients":[{"description":"oil"}],"directive":"cook"}`, want: `scenario "test" location state is required`},
		{name: "ingredients", raw: `{"name":"test","location":{"state":"WA"},"date":"2026-07-16","directive":"cook"}`, want: `scenario "test" requires ingredients`},
		{name: "ingredient description", raw: `{"name":"test","location":{"state":"WA"},"date":"2026-07-16","ingredients":[{"id":"oil"}],"directive":"cook"}`, want: `scenario "test" ingredient 1 description is required`},
		{name: "directions", raw: `{"name":"test","location":{"state":"WA"},"date":"2026-07-16","ingredients":[{"description":"oil"}]}`, want: `scenario "test" requires a directive or instructions`},
		{name: "date", raw: `{"name":"test","location":{"state":"WA"},"date":"July 16","ingredients":[{"description":"oil"}],"directive":"cook"}`, want: `scenario "test" date must use YYYY-MM-DD:`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseScenario(tt.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func testScenarioJSON() string {
	return `{
		"name":"split marinade",
		"location":{"state":"WA"},
		"date":"2026-07-16",
		"ingredients":[
			{"id":"chicken-1","description":"Chicken Thighs"},
			{"id":"oil-1","description":"Extra Virgin Olive Oil"}
		],
		"directive":"Create a dinner that divides the oil between a marinade and roasting.",
		"instructions":"Keep it practical."
	}`
}
