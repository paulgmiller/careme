package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocationStore struct {
	locations    []locations.Location
	inventoryIDs map[string]bool
	err          error
}

func (f fakeLocationStore) GetLocationsByZip(context.Context, string) ([]locations.Location, error) {
	return f.locations, f.err
}

func (f fakeLocationStore) HasInventory(locationID string) bool {
	return f.inventoryIDs[locationID]
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

	got, err := firstInventoryStores(t.Context(), store, "98101", 2)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "70500001", got[0].ID)
	assert.Equal(t, "safeway_2", got[1].ID)
}

func TestFirstInventoryStoresRequiresInventoryBackedStore(t *testing.T) {
	store := fakeLocationStore{
		locations: []locations.Location{{ID: "aldi_1", Name: "Aldi"}},
	}

	_, err := firstInventoryStores(t.Context(), store, "98101", 5)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no inventory-backed grocery stores")
}

func TestFirstInventoryStoresWrapsLookupError(t *testing.T) {
	want := errors.New("zip lookup failed")
	store := fakeLocationStore{err: want}

	_, err := firstInventoryStores(t.Context(), store, "98101", 5)

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
				{Cuisine: "Korean", AnchorIngredient: "chicken thighs", Technique: "sheet pan", SideVegetable: "broccoli"},
				{Cuisine: "Thai", AnchorIngredient: "rice noodles", Technique: "stir fry", SideVegetable: "snap peas", Fancy: true},
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

type blockingStaplesService struct {
	started chan string
	release chan struct{}
}

func (s *blockingStaplesService) FetchStaples(_ context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error) {
	s.started <- p.Location.ID
	<-s.release
	return []ai.InputIngredient{{ProductID: p.Location.ID + "-ingredient", Description: "seasonal greens"}}, nil
}

type blockingMenuPlanner struct {
	started chan string
	release chan struct{}
}

func (p *blockingMenuPlanner) CreateMenuPlan(_ context.Context, location *locations.Location, _ []ai.InputIngredient, _ []string, _ time.Time, _ []string, _ int) (*ai.MenuPlan, error) {
	p.started <- location.ID
	<-p.release
	return &ai.MenuPlan{Plans: []ai.RecipePlan{{Cuisine: "Japanese", AnchorIngredient: location.ID, Technique: "grill"}}}, nil
}

func waitForStarted(t *testing.T, ch <-chan string, want ...string) {
	t.Helper()
	seen := make(map[string]bool, len(want))
	for range want {
		select {
		case id := <-ch:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for starts; saw %v want %v", seen, want)
		}
	}
	for _, id := range want {
		assert.True(t, seen[id], "expected %s to start", id)
	}
}
