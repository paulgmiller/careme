package campaigns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/require"
)

func TestAdvertisedRecipeGenerationRouteKicksMissingShoppingLists(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	kicker := &advertisedGenerationKickstarterStub{}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, kicker, cacheStore)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Len(t, kicker.hashes, len(AdvertisedRecipeLocations()))
	require.Contains(t, response.Body.String(), `"kicked"`)
}

func TestAdvertisedRecipeGenerationRouteRefreshesManifestFromCachedLists(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	kicker := &advertisedGenerationKickstarterStub{present: true, rio: recipes.IO(cacheStore)}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, kicker, cacheStore)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)

	manifest, err := LoadAdvertisedRecipeManifest(t.Context(), cacheStore)
	require.NoError(t, err)
	require.Len(t, manifest.Entries, len(AdvertisedRecipeLocations()))
	require.Equal(t, kicker.hashes[0], manifest.Entries[0].ShoppingListHash)
}

func TestAdvertisedRecipeGenerationRouteOnlyAcceptsPOST(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, &advertisedGenerationKickstarterStub{}, cacheStore)

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

type advertisedGenerationKickstarterStub struct {
	hashes  []string
	present bool
	rio     interface {
		SaveShoppingList(ctx context.Context, shoppingList *ai.ShoppingList, hash string) error
	}
}

func (s *advertisedGenerationKickstarterStub) KickGenerationIfNotPresent(ctx context.Context, p *recipes.GeneratorParams) (recipes.GenerationKickResult, error) {
	hash := p.Hash()
	s.hashes = append(s.hashes, hash)
	if s.present && s.rio != nil {
		err := s.rio.SaveShoppingList(ctx, &ai.ShoppingList{
			Recipes: []ai.Recipe{
				{
					Title:        "Campaign Pasta",
					Description:  "A simple campaign recipe.",
					CookTime:     "30 minutes",
					CostEstimate: "$20",
					Ingredients:  []ai.Ingredient{{Name: "Pasta", Quantity: "1 lb"}},
					Instructions: []string{"Boil pasta."},
				},
			},
		}, hash)
		if err != nil {
			return recipes.GenerationKickResult{}, err
		}
	}
	return recipes.GenerationKickResult{Hash: hash, Kicked: !s.present}, nil
}
