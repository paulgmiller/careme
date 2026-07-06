package campaigns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/require"
)

func TestAdvertisedRecipeGenerationRouteKicksAdvertisedLocations(t *testing.T) {
	kicker := &advertisedGenerationKickstarterStub{}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, kicker)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, kicker.params, len(AdvertisedRecipeLocations()))

	locationsByID := make(map[string]*locations.Location, len(kicker.params))
	for _, params := range kicker.params {
		locationsByID[params.Location.ID] = params.Location
	}
	require.Contains(t, locationsByID, "70100658")
	require.Equal(t, "Hydrated 70100658", locationsByID["70100658"].Name)
	require.Equal(t, "70100658 Market St", locationsByID["70100658"].Address)
}

func TestAdvertisedRecipeGenerationRouteReturnsError(t *testing.T) {
	kicker := &advertisedGenerationKickstarterStub{err: errors.New("no soup")}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, kicker)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "kick generation: no soup")
}

func TestAdvertisedRecipeGenerationRouteOnlyAcceptsPOST(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, advertisedLocationStoreStub{}, &advertisedGenerationKickstarterStub{})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

type advertisedLocationStoreStub struct{}

func (advertisedLocationStoreStub) GetLocationByID(_ context.Context, locationID string) (*locations.Location, error) {
	return &locations.Location{
		ID:      locationID,
		Name:    "Hydrated " + locationID,
		Address: locationID + " Market St",
		ZipCode: "98101",
	}, nil
}

type advertisedGenerationKickstarterStub struct {
	params []*recipes.GeneratorParams
	err    error
}

func (s *advertisedGenerationKickstarterStub) KickGenerationIfNotPresent(_ context.Context, p *recipes.GeneratorParams) error {
	s.params = append(s.params, p)
	return s.err
}
