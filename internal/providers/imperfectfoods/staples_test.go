package imperfectfoods_test

import (
	"context"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/locations/geo"
	"careme/internal/providers/imperfectfoods"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticCatalogRoutingAndPromptOutput(t *testing.T) {
	backend := imperfectfoods.NewStaplesProvider()
	provider := recipes.NewStaplesProvider([]recipes.StaplesBackend{backend})
	food, err := provider.FetchStaples(t.Context(), "imperfectfoods_demo")
	require.NoError(t, err)
	require.Len(t, food, 483)
	wines, err := provider.FetchWines(t.Context(), "imperfectfoods_demo", []string{"red"})
	require.NoError(t, err)
	require.Len(t, wines, 28)
	seen := make(map[string]bool)
	for _, item := range append(food, wines...) {
		assert.False(t, seen[item.ProductID], "duplicate %s", item.ProductID)
		seen[item.ProductID] = true
		assert.NotEmpty(t, item.Description)
		require.NotNil(t, item.PriceRegular)
		assert.Positive(t, *item.PriceRegular)
	}
	var prompt strings.Builder
	require.NoError(t, ai.InputIngredientsToTSV(food, &prompt))
	assert.Contains(t, prompt.String(), "Organic Cauliflower")
	assert.Contains(t, prompt.String(), "Beef Bone-In Ribeye Bundle, 2X16 Oz, 32 Oz\t32 Oz\t36.99\t36.99")
	assert.NotContains(t, prompt.String(), "Carménère")
	prompt.Reset()
	require.NoError(t, ai.InputIngredientsToTSV(wines, &prompt))
	assert.Contains(t, prompt.String(), "2023 Montañero Private Reserve Carménère\t750 ml\t18.99\t18.99")
}

func TestStaticProviderLocationAndErrors(t *testing.T) {
	backend := imperfectfoods.NewStaplesProvider()
	locations := imperfectfoods.NewLocationBackend()
	location, err := locations.GetLocationByID(t.Context(), "imperfectfoods_demo")
	require.NoError(t, err)
	assert.Equal(t, "Imperfect Foods", location.Chain)
	assert.True(t, locations.HasInventory(location.ID))
	assert.False(t, locations.HasInventory("other"))
	found, err := locations.GetLocationsByCoordinates(t.Context(), geo.Coordinate{Lat: 47.6, Lon: -122.3})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, *location, found[0])
	_, err = locations.GetLocationsByCoordinates(t.Context(), geo.Coordinate{})
	require.Error(t, err)
	_, err = locations.GetLocationByID(t.Context(), "other")
	require.ErrorContains(t, err, "invalid Imperfect Foods location")
	_, err = backend.FetchStaples(t.Context(), "other")
	require.ErrorContains(t, err, "invalid Imperfect Foods location")
	_, err = backend.FetchWines(t.Context(), "other", nil)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = backend.FetchStaples(ctx, location.ID)
	require.ErrorIs(t, err, context.Canceled)
	_, err = backend.FetchWines(ctx, location.ID, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, err = locations.GetLocationByID(ctx, location.ID)
	require.ErrorIs(t, err, context.Canceled)
	_, err = locations.GetLocationsByCoordinates(ctx, geo.Coordinate{Lat: 47.6, Lon: -122.3})
	require.ErrorIs(t, err, context.Canceled)
}
