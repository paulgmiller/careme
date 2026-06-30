package campaigns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/require"
)

func TestAdvertisedRecipeGenerationRouteGeneratesManifest(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	rio := recipes.IO(cacheStore)
	generator := NewAdvertisedRecipeGenerator(advertisedLocationStoreStub{}, advertisedGeneratorStub{}, rio, cacheStore)

	mux := http.NewServeMux()
	generator.Register(mux)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	manifest, err := LoadAdvertisedRecipeManifest(t.Context(), cacheStore)
	require.NoError(t, err)
	require.Len(t, manifest.Entries, len(AdvertisedRecipeLocations()))
	require.Empty(t, manifest.Failures)
	require.NotEmpty(t, manifest.Entries[0].ShoppingListHash)
	require.Len(t, manifest.Entries[0].RecipeHashes, 1)
}

func TestAdvertisedRecipeGenerationRouteOnlyAcceptsPOST(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	generator := NewAdvertisedRecipeGenerator(advertisedLocationStoreStub{}, advertisedGeneratorStub{}, recipes.IO(cacheStore), cacheStore)

	mux := http.NewServeMux()
	generator.Register(mux)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

type advertisedLocationStoreStub struct{}

func (advertisedLocationStoreStub) GetLocationByID(_ context.Context, locationID string) (*locations.Location, error) {
	return &locations.Location{
		ID:      locationID,
		Name:    "Campaign Store",
		ZipCode: "98101",
	}, nil
}

type advertisedGeneratorStub struct{}

func (advertisedGeneratorStub) GenerateRecipes(_ context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title:        "Campaign Pasta " + p.Location.ID,
				Description:  "A simple campaign recipe.",
				CookTime:     "30 minutes",
				CostEstimate: "$20",
				Ingredients:  []ai.Ingredient{{Name: "Pasta", Quantity: "1 lb"}},
				Instructions: []string{"Boil pasta."},
			},
		},
	}, nil
}

func (advertisedGeneratorStub) RegenerateRecipe(context.Context, []string, string) (*ai.Recipe, error) {
	return nil, nil
}

func (advertisedGeneratorStub) AskQuestion(context.Context, string, string) (*ai.QuestionResponse, error) {
	return nil, nil
}

func (advertisedGeneratorStub) PickAWine(context.Context, string, ai.Recipe, time.Time) (*ai.WineSelection, error) {
	return nil, nil
}
