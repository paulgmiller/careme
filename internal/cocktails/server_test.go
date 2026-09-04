package cocktails

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/seasons"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLocations struct{}

func (fakeLocations) GetLocationByID(context.Context, string) (*locationtypes.Location, error) {
	return &locationtypes.Location{ID: "70500874", Name: "Kroger", State: "WA"}, nil
}

func TestCocktailFlow(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	storage := cache.NewFileCache(t.TempDir())
	server := New(Mock{}, Mock{}, fakeLocations{}, storage, locations.LoadCentroids())
	mux := http.NewServeMux()
	server.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/cocktails", strings.NewReader(url.Values{"location": {"70500874"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	server.Wait()
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest("GET", response.Header().Get("Location"), nil))
	require.Equal(t, 200, page.Code)
	assert.Equal(t, 3, strings.Count(page.Body.String(), "<article"))
	assert.Contains(t, page.Body.String(), "Makes 1 drink")
	assert.Contains(t, page.Body.String(), "https://www.kroger.com/p/item/gin")
	assert.NotContains(t, page.Body.String(), "Wine")
	// A fresh handler can recover the persisted menu without another generation.
	reloaded := New(Mock{}, Mock{}, fakeLocations{}, storage, locations.LoadCentroids())
	page = httptest.NewRecorder()
	reloaded.page(page, httptest.NewRequest("GET", "/cocktails?location=70500874", nil))
	assert.Contains(t, page.Body.String(), "Garden Gin Sour")
}

func TestUnsupportedStore(t *testing.T) {
	server := New(Mock{}, Mock{}, fakeLocations{}, cache.NewFileCache(t.TempDir()), locations.LoadCentroids())
	for _, id := range []string{"", "wholefoods_123", "../bad"} {
		response := httptest.NewRecorder()
		server.start(response, httptest.NewRequest("POST", "/cocktails?location="+url.QueryEscape(id), nil))
		assert.Equal(t, 400, response.Code)
	}
}

func TestValidateMenu(t *testing.T) {
	catalog, err := (Mock{}).FetchCocktailIngredients(t.Context(), "70500874", seasons.Fall)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		change func(*ai.CocktailMenu)
	}{
		{"missing drink", func(m *ai.CocktailMenu) { m.Cocktails = m.Cocktails[:2] }},
		{"unknown product", func(m *ai.CocktailMenu) { m.Cocktails[0].Ingredients[0].ProductID = "invented" }},
		{"missing steps", func(m *ai.CocktailMenu) { m.Cocktails[0].Instructions = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			menu, err := (Mock{}).GenerateCocktails(t.Context(), "", catalog)
			require.NoError(t, err)
			tc.change(menu)
			require.Error(t, ai.ValidateCocktails(menu, catalog))
		})
	}
}

func TestValidateMenuAllowsUnlinkedIngredients(t *testing.T) {
	catalog, err := (Mock{}).FetchCocktailIngredients(t.Context(), "70500874", seasons.Fall)
	require.NoError(t, err)
	for _, name := range []string{"ice for shaking", "Ice cubes, plus more for serving", "cold water", "granulated sugar (for syrup)", "Gin"} {
		t.Run(name, func(t *testing.T) {
			menu, err := (Mock{}).GenerateCocktails(t.Context(), "", catalog)
			require.NoError(t, err)
			menu.Cocktails[0].Ingredients[0] = ai.Ingredient{Name: name, Quantity: "1 oz"}
			require.NoError(t, ai.ValidateCocktails(menu, catalog))
		})
	}
}

func (fakeLocations) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locationtypes.Location, error) {
	return []locationtypes.Location{{ID: "70500874", Name: "Kroger", Address: "123 Main Street"}, {ID: "wholefoods_123", Name: "Whole Foods"}}, nil
}

func TestCocktailLocationPicker(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	server := New(Mock{}, Mock{}, fakeLocations{}, cache.NewFileCache(t.TempDir()), locations.LoadCentroids())
	mux := http.NewServeMux()
	server.Register(mux)
	for _, tc := range []struct {
		name, path       string
		status           int
		contains, absent string
	}{
		{"landing", "/cocktails", http.StatusOK, "Where are you shopping?", "Stores near"},
		{"search", "/cocktails?zip=98101", http.StatusOK, "123 Main Street", "Whole Foods"},
		{"invalid ZIP", "/cocktails?zip=bad", http.StatusBadRequest, "Enter a valid US ZIP code", "Find three cocktails"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			assert.Equal(t, tc.status, response.Code)
			assert.Contains(t, response.Body.String(), tc.contains)
			assert.NotContains(t, response.Body.String(), tc.absent)
			assert.Contains(t, response.Body.String(), `action="/cocktails"`)
			assert.NotContains(t, response.Body.String(), `action="/recipes"`)
		})
	}
}
