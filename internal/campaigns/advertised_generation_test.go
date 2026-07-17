package campaigns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/locations"
	"careme/internal/logsetup"
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
	require.Len(t, kicker.contexts, len(AdvertisedRecipeLocations()))
	for _, ctx := range kicker.contexts {
		sessionID, ok := logsetup.SessionIDFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "campaign_ads", sessionID)
		userID, ok := logsetup.UserIDFromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "campaign_ads", userID)
	}
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
	params   []*recipes.GeneratorParams
	contexts []context.Context
}

func (s *advertisedGenerationKickstarterStub) KickGenerationIfNotPresent(ctx context.Context, p *recipes.GeneratorParams) {
	s.params = append(s.params, p)
	s.contexts = append(s.contexts, ctx)
}
