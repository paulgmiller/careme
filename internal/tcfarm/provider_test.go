package tcfarm

import (
	"testing"

	"careme/internal/locations/geo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrozenCatalog(t *testing.T) {
	p := Provider{}
	items, err := p.FetchStaples(t.Context(), LocationID)
	require.NoError(t, err)
	require.Len(t, items, 31) // 24 unique produce items, one substitution, six add-ons.
	names := map[string][]string{}
	ids := map[string]bool{}
	for _, item := range items {
		require.NotEmpty(t, item.ProductID)
		require.False(t, ids[item.ProductID], "duplicate product %s", item.ProductID)
		ids[item.ProductID] = true
		names[item.Description] = item.Categories
		assert.Nil(t, item.PriceRegular)
		assert.Nil(t, item.PriceSale)
	}
	assert.ElementsMatch(t, []string{"Produce", "Seasonal", "Small Seasonal", "Low Carb"}, names["Cucumbers"])
	assert.ElementsMatch(t, []string{"Produce", "Fruit", "Small Fruit"}, names["Red Bartlett Pears"])
	assert.Contains(t, names["Blueberries"], "Fruit substitution")
	for _, name := range []string{"TC Farm Pork Tenderloin", "TC Farm Bratwurst", "TC Farm Italian Sausage", "TC Farm Chicken Thighs – Ranger", "Sour Cream", "Oddbird GSM NA Red Wine (dealcoholized)"} {
		assert.Contains(t, names[name], "Recommended add-on")
	}
	for _, name := range []string{"Butter", "Olive Oil", "Cream", "Milk", "Figs"} {
		assert.NotContains(t, names, name)
	}
	// Callers must not be able to alter the shared inventory.
	items[0].Description = "changed"
	items[0].Categories[0] = "changed"
	fresh, err := p.FetchStaples(t.Context(), LocationID)
	require.NoError(t, err)
	assert.Equal(t, "Butternut Squash", fresh[0].Description)
	assert.Equal(t, "Produce", fresh[0].Categories[0])
	wines, err := p.FetchWines(t.Context(), LocationID, []string{"red"})
	require.NoError(t, err)
	require.Len(t, wines, 1)
	assert.Equal(t, fresh[len(fresh)-1], wines[0])
}

func TestDemoLocation(t *testing.T) {
	p := Provider{}
	loc, err := p.GetLocationByID(t.Context(), LocationID)
	require.NoError(t, err)
	assert.Equal(t, LocationID, loc.ID)
	assert.Equal(t, "TC Farm · September 21–26", loc.Name)
	assert.NoError(t, loc.Coordinate().Valid())
	assert.Equal(t, "55401", loc.ZipCode)
	assert.Equal(t, geo.Coordinate{Lat: 44.985367, Lon: -93.270208}, loc.Coordinate())
	assert.True(t, p.HasInventory(LocationID))
	nearby, err := p.GetLocationsByCoordinates(t.Context(), geo.Coordinate{Lat: 45, Lon: -94})
	require.NoError(t, err)
	assert.Empty(t, nearby)
	for _, id := range []string{"", "tcfarm_other", LocationID + "_other"} {
		assert.False(t, p.IsID(id))
		assert.False(t, p.HasInventory(id))
		_, err = p.GetLocationByID(t.Context(), id)
		require.Error(t, err)
		_, err = p.FetchStaples(t.Context(), id)
		require.Error(t, err)
		_, err = p.FetchWines(t.Context(), id, []string{"red"})
		require.Error(t, err)
	}
}
