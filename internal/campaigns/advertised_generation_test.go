package campaigns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/recipes"

	"github.com/stretchr/testify/require"
)

func TestAdvertisedRecipeGenerationRouteKicksAdvertisedLocations(t *testing.T) {
	kicker := &advertisedGenerationKickstarterStub{}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, kicker)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, kicker.params, len(AdvertisedRecipeLocations()))
	require.Equal(t, "wholefoods_10153", kicker.params[0].Location.ID)
}

func TestAdvertisedRecipeGenerationRouteReturnsError(t *testing.T) {
	kicker := &advertisedGenerationKickstarterStub{err: errors.New("no soup")}

	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, kicker)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "kick generation: no soup")
}

func TestAdvertisedRecipeGenerationRouteOnlyAcceptsPOST(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAdvertisedRecipeGeneration(mux, &advertisedGenerationKickstarterStub{})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/campaigns/advertised-recipes/generate", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

type advertisedGenerationKickstarterStub struct {
	params []*recipes.GeneratorParams
	err    error
}

func (s *advertisedGenerationKickstarterStub) KickGenerationIfNotPresent(_ context.Context, p *recipes.GeneratorParams) error {
	s.params = append(s.params, p)
	return s.err
}
