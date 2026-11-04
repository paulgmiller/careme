package localyokels_test

import (
	"context"
	"errors"
	"net/http"
	"os"
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

type catalog struct {
	t         *testing.T
	selection localfoodmarketplace.Selection
	items     []ai.InputIngredient
	err       error
}

func (c catalog) Fetch(_ context.Context, selection localfoodmarketplace.Selection) ([]ai.InputIngredient, error) {
	assert.Equal(c.t, c.selection, selection)
	return c.items, c.err
}

func TestRoutingAndLocation(t *testing.T) {
	b := localyokels.NewStaplesProvider(catalog{t: t, items: []ai.InputIngredient{{ProductID: "beef_farm", Description: "Beef", Size: "1 lb", PriceRegular: new(float32(10))}}})
	p := recipes.NewStaplesProvider([]recipes.StaplesBackend{b})
	items, err := p.FetchStaples(t.Context(), "localfoodmarektplace_localyokels")
	require.NoError(t, err)
	require.Len(t, items, 1)
	_, err = b.FetchStaples(t.Context(), "localyokels_invalid")
	require.Error(t, err)
	l := localyokels.NewLocationBackend()
	location, err := l.GetLocationByID(t.Context(), "localfoodmarektplace_localyokels")
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

func TestWineRouting(t *testing.T) {
	wines := []ai.InputIngredient{{ProductID: "wine_farm", Description: "Sparkling Rosé Wine", Size: "750 ml", PriceRegular: new(float32(24))}}
	failure := errors.New("catalog unavailable")
	for _, err := range []error{nil, failure} {
		b := localyokels.NewStaplesProvider(catalog{t: t, selection: localfoodmarketplace.Selection{CategoryIDs: []int{1220}}, items: wines, err: err})
		p := recipes.NewStaplesProvider([]recipes.StaplesBackend{b})
		got, gotErr := p.FetchWines(t.Context(), "localfoodmarektplace_localyokels", []string{"red", "sparkling"})
		if err != nil {
			require.ErrorIs(t, gotErr, failure)
			require.ErrorContains(t, gotErr, "fetch Local Yokels wines")
			assert.Nil(t, got)
		} else {
			require.NoError(t, gotErr)
			assert.Equal(t, wines, got)
		}
		got, gotErr = b.FetchWines(t.Context(), "localyokels_unknown", []string{"red"})
		require.ErrorContains(t, gotErr, "invalid Local Yokels location")
		assert.Nil(t, got)
	}
}

func TestDailyIdentity(t *testing.T) {
	p := localyokels.NewIdentityProvider()
	assert.True(t, p.IsID("localfoodmarektplace_localyokels"))
	assert.False(t, p.IsID("localyokels_unknown"))
	assert.False(t, p.IsID("localfoodmarektplace_otherstore"))
	assert.False(t, p.IsID("localyokels_market"))
	date := time.Date(2026, time.November, 3, 9, 0, 0, 0, time.FixedZone("Pacific", -8*3600))
	hash := func(d time.Time) string {
		return string(p.HashForStore("localfoodmarektplace_localyokels", d).Sum(nil))
	}
	assert.Equal(t, hash(date), hash(date.Add(10*time.Hour)))
	assert.NotEqual(t, hash(date), hash(date.Add(-time.Minute)))
	assert.NotEqual(t, hash(date), hash(date.AddDate(0, 0, 1)))
}

func TestLiveLocalYokels(t *testing.T) {
	if os.Getenv("LOCALYOKELS_LIVE") != "1" {
		t.Skip("set LOCALYOKELS_LIVE=1 for public catalog smoke test")
	}
	provider := localyokels.NewStaplesProvider(localyokels.NewClient(http.DefaultClient))
	items, err := provider.FetchStaples(t.Context(), "localfoodmarektplace_localyokels")
	require.NoError(t, err)
	require.NotEmpty(t, items)
	t.Logf("Fetched %d staple products", len(items))
	drinks, err := provider.FetchWines(t.Context(), "localfoodmarektplace_localyokels", []string{"red"})
	require.NoError(t, err)
	require.NotEmpty(t, drinks)
	for _, drink := range drinks {
		assert.Contains(t, drink.Categories, "Beer, Wine, and Cider")
	}
	t.Logf("Fetched %d beer, wine, and cider products", len(drinks))
}
