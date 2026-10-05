package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/locations/geo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationStore struct {
	locations    []locations.Location
	inventoryIDs map[string]bool
	err          error
}

func (f fakeLocationStore) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locations.Location, error) {
	return f.locations, f.err
}

func (f fakeLocationStore) HasInventory(locationID string) bool {
	return f.inventoryIDs[locationID]
}

func (f fakeLocationStore) GetLocationByID(_ context.Context, id string) (*locations.Location, error) {
	for _, loc := range f.locations {
		if loc.ID == id {
			return &loc, f.err
		}
	}
	return nil, errors.New("location not found")
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		message string
	}{
		{nil, "exactly one"},
		{[]string{"-zip", "98101", "-location", "70500874"}, "exactly one"},
		{[]string{"-location", "  "}, "exactly one"},
		{[]string{"-location", "70500874", "-plans", "0"}, "-plans"},
		{[]string{"-zip", "98101", "-plans", "-1"}, "-plans"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			require.ErrorContains(t, run(t.Context(), tc.args, &bytes.Buffer{}), tc.message)
		})
	}
}

func TestSelectStores(t *testing.T) {
	store := fakeLocationStore{locations: []locations.Location{{ID: "70500874"}, {ID: "unsupported"}}, inventoryIDs: map[string]bool{"70500874": true}}
	got, err := selectStores(t.Context(), store, "70500874", "", 5)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "70500874", got[0].ID)
	_, err = selectStores(t.Context(), store, "missing", "", 5)
	require.ErrorContains(t, err, "location not found")
	_, err = selectStores(t.Context(), store, "unsupported", "", 5)
	require.ErrorContains(t, err, "no inventory support")
	got, err = selectStores(t.Context(), store, "", "98101", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	_, err = selectStores(t.Context(), store, "", "invalid", 1)
	require.ErrorContains(t, err, "coordinates not found")
}

type countingMenuPlanner struct{ calls atomic.Int32 }

func (p *countingMenuPlanner) CreateMenuPlan(_ context.Context, _ *locations.Location, _ []ai.InputIngredient, _ []string, _ time.Time, _ []string, count int) (*ai.MenuPlan, error) {
	p.calls.Add(1)
	return &ai.MenuPlan{Plans: make([]ai.RecipePlan, count)}, nil
}

func TestMakeMenuPlansCount(t *testing.T) {
	for _, count := range []int{1, 4} {
		planner := &countingMenuPlanner{}
		service := planService{planner: planner, staples: mockStaplesService{}, pantry: mockPantryService{}}
		plans, err := makeMenuPlans(t.Context(), service, locations.Location{ID: "70500874"}, time.Now(), "", 3, count)
		require.NoError(t, err)
		assert.Equal(t, int32(count), planner.calls.Load())
		assert.Len(t, plans, count*3)
	}
}

func TestFirstInventoryStoresFiltersAndLimits(t *testing.T) {
	store := fakeLocationStore{
		locations: []locations.Location{
			{ID: "aldi_1", Name: "Aldi"},
			{ID: "70500001", Name: "Kroger One"},
			{ID: "publix_1", Name: "Publix"},
			{ID: "safeway_2", Name: "Safeway Two"},
			{ID: "70500003", Name: "Kroger Three"},
		},
		inventoryIDs: map[string]bool{
			"70500001":  true,
			"safeway_2": true,
			"70500003":  true,
		},
	}

	got, err := firstInventoryStores(t.Context(), store, geo.Coordinate{Lat: 47, Lon: -122}, 2)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "70500001", got[0].ID)
	assert.Equal(t, "safeway_2", got[1].ID)
}

func TestFirstInventoryStoresRequiresInventoryBackedStore(t *testing.T) {
	store := fakeLocationStore{
		locations: []locations.Location{{ID: "aldi_1", Name: "Aldi"}},
	}

	_, err := firstInventoryStores(t.Context(), store, geo.Coordinate{Lat: 47, Lon: -122}, 5)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no inventory-backed grocery stores")
}

func TestFirstInventoryStoresWrapsLookupError(t *testing.T) {
	want := errors.New("zip lookup failed")
	store := fakeLocationStore{err: want}

	_, err := firstInventoryStores(t.Context(), store, geo.Coordinate{Lat: 47, Lon: -122}, 5)

	require.ErrorIs(t, err, want)
}

func TestWriteMenuPlansHumanReadable(t *testing.T) {
	var out bytes.Buffer
	results := []storeMenuPlan{
		{
			Location: locations.Location{
				ID:      "70500001",
				Chain:   "Kroger",
				Name:    "Downtown",
				Address: "1 Market St",
				State:   "WA",
				ZipCode: "98101",
			},
			Date: time.Date(2026, time.May, 13, 0, 0, 0, 0, time.UTC),
			Plan: &ai.MenuPlan{Plans: []ai.RecipePlan{
				{Cuisine: "Korean", AnchorIngredient: "chicken thighs", DishFormat: "sheet pan", SideVegetable: "broccoli"},
				{Cuisine: "Thai", AnchorIngredient: "rice noodles", DishFormat: "stir fry", SideVegetable: "snap peas", Fancy: true},
			}},
		},
		{
			Location: locations.Location{ID: "safeway_2", Chain: "Safeway"},
			Err:      errors.New("staples unavailable"),
		},
	}

	err := writeMenuPlans(&out, "98101", results)

	require.NoError(t, err)
	rendered := out.String()
	for _, want := range []string{
		"Menu plans for 98101",
		"1. Kroger - Downtown",
		"Address: 1 Market St, WA, 98101",
		"Date: 2026-05-13",
		"Plan:",
		"Korean with chicken thighs, sheet pan, side veg: broccoli",
		"Thai with rice noodles, stir fry, side veg: snap peas (fancier)",
		"2. Safeway",
		"Could not make a menu plan: staples unavailable",
	} {
		assert.True(t, strings.Contains(rendered, want), "rendered output missing %q:\n%s", want, rendered)
	}
	assert.NotContains(t, rendered, "Recipes:")
	assert.NotContains(t, rendered, "Ingredients:")
	assert.NotContains(t, rendered, "Steps:")
}

func TestPantryCategoryNeighbors(t *testing.T) {
	var pantry []ai.InputIngredient
	for i := 0; i < 7; i++ {
		pantry = append(pantry, ai.InputIngredient{Categories: []string{"spices"}, Embedding: ai.IngredientEmbedding{float64(i), 0}})
	}
	pantry = append(pantry, ai.InputIngredient{ProductID: "dairy", Categories: []string{"dairy", "international"}, Embedding: ai.IngredientEmbedding{10, 0}})
	spices, err := pantryCategoryNeighbors(ai.IngredientEmbedding{1, 0}, pantry, "spices")
	require.NoError(t, err)
	require.Len(t, spices, 5)
	assert.Equal(t, 6.0, spices[0].Similarity)
	assert.Equal(t, 2.0, spices[4].Similarity)
	for _, category := range []string{"dairy", "international"} {
		got, err := pantryCategoryNeighbors(ai.IngredientEmbedding{1, 0}, pantry, category)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "dairy", got[0].Ingredient.ProductID)
	}
}

func TestFilterMenuIngredientsDropsLowGrades(t *testing.T) {
	ingredients := []ai.InputIngredient{
		{ProductID: "ungraded"},
		{ProductID: "good", Grade: &ai.IngredientGrade{Score: 7}},
		{ProductID: "bad", Grade: &ai.IngredientGrade{Score: 6}},
	}

	got := filterMenuIngredients(ingredients)

	require.Len(t, got, 2)
	assert.Equal(t, "ungraded", got[0].ProductID)
	assert.Equal(t, "good", got[1].ProductID)
}

func TestPantryQueryStripsCatalogBrands(t *testing.T) {
	catalog := []ai.InputIngredient{
		{Brand: "Kroger"},
		{Brand: "Simple Truth"},
		{Brand: "Simple Truth Organic"},
		{Brand: "Thai"},
		{Brand: ""},
		{Brand: "365+"},
	}
	for _, tc := range []struct{ anchor, side, want string }{
		{"Kroger® Ground Pork", "Fresh Red Hothouse Bell Pepper", "Thai, Ground Pork, Fresh Red Hothouse Bell Pepper"},
		{"simple truth organic™ Chicken", "KROGER Broccoli", "Thai, Chicken, Broccoli"},
		{"pork", "365+™ Spinach", "Thai, pork, Spinach"},
		{"Krogerish Pork", "Thai Basil", "Thai, Krogerish Pork, Basil"},
		{"Ground Pork", "", "Thai, Ground Pork"},
	} {
		t.Run(tc.anchor, func(t *testing.T) {
			plan := ai.RecipePlan{Cuisine: "Thai", AnchorIngredient: tc.anchor, SideVegetable: tc.side}
			assert.Equal(t, tc.want, pantryQuery(plan, catalog))
			assert.Equal(t, tc.anchor, plan.AnchorIngredient)
		})
	}
}
