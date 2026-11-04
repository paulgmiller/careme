package localyokels_test

import (
	"context"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/localfoodmarketplace"
	"careme/internal/locations/geo"
	"careme/internal/providers/localyokels"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type catalog struct{ t *testing.T }

func (c catalog) Fetch(_ context.Context, selection localfoodmarketplace.Selection) ([]ai.InputIngredient, error) {
	assert.Equal(c.t, localfoodmarketplace.Selection{CategoryIDs: []int{1230, 1232, 1222, 1225}, SubcategoryIDs: []int{1289}}, selection)
	return []ai.InputIngredient{{ProductID: "beef_farm", Description: "Beef", Size: "1 lb", PriceRegular: new(float32(10))}}, nil
}

func TestRoutingAndLocation(t *testing.T) {
	b := localyokels.NewStaplesProvider(catalog{t})
	p := recipes.NewStaplesProvider([]recipes.StaplesBackend{b})
	items, err := p.FetchStaples(t.Context(), "localyokels_market")
	require.NoError(t, err)
	require.Len(t, items, 1)
	_, err = b.FetchStaples(t.Context(), "localyokels_invalid")
	require.Error(t, err)
	_, err = p.FetchWines(t.Context(), "localyokels_market", []string{"red"})
	require.ErrorContains(t, err, "not supported")
	l := localyokels.NewLocationBackend()
	location, err := l.GetLocationByID(t.Context(), "localyokels_market")
	require.NoError(t, err)
	assert.Equal(t, "Local Yokels", location.Chain)
	assert.Contains(t, location.Address, "8416")
	assert.True(t, l.HasInventory(location.ID))
	assert.False(t, l.HasInventory("localyokels_unknown"))
	found, err := l.GetLocationsByCoordinates(t.Context(), geo.Coordinate{Lat: 47.6, Lon: -122.3})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, *location, found[0])
	_, err = l.GetLocationByID(t.Context(), "localyokels_unknown")
	require.Error(t, err)
	_, err = l.GetLocationsByCoordinates(t.Context(), geo.Coordinate{})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = l.GetLocationsByCoordinates(ctx, geo.Coordinate{Lat: 47.6, Lon: -122.3})
	require.ErrorIs(t, err, context.Canceled)
}

func TestDailyIdentity(t *testing.T) {
	p := localyokels.NewIdentityProvider()
	assert.True(t, p.IsID("localyokels_market"))
	assert.False(t, p.IsID("localyokels_unknown"))
	date := time.Date(2026, time.November, 3, 9, 0, 0, 0, time.FixedZone("Pacific", -8*3600))
	hash := func(d time.Time) string { return string(p.HashForStore("localyokels_market", d).Sum(nil)) }
	assert.Equal(t, hash(date), hash(date.Add(10*time.Hour)))
	assert.NotEqual(t, hash(date), hash(date.Add(-time.Minute)))
	assert.NotEqual(t, hash(date), hash(date.AddDate(0, 0, 1)))
}
