package campaigns

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"careme/internal/recipes"

	"github.com/stretchr/testify/require"
)

func TestIssaquahRedirect(t *testing.T) {
	tests := []struct {
		name          string
		request       string
		expectedQuery url.Values
	}{
		{
			name:    "sets campaign location and help",
			request: "/c/issaquah",
			expectedQuery: url.Values{
				"location":             {"70100658"},
				recipes.HelpQueryParam: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
			},
		},
		{
			name:    "preserves attribution parameters",
			request: "/c/issaquah?utm_source=facebook&utm_campaign=carts",
			expectedQuery: url.Values{
				"location":             {"70100658"},
				recipes.HelpQueryParam: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
				"utm_source":           {"facebook"},
				"utm_campaign":         {"carts"},
			},
		},
		{
			name:    "overrides incoming location",
			request: "/c/issaquah?location=other&utm_source=facebook",
			expectedQuery: url.Values{
				"location":             {"70100658"},
				recipes.HelpQueryParam: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
				"utm_source":           {"facebook"},
			},
		},
		{
			name:    "overrides incoming help",
			request: "/c/issaquah?help=Custom+note",
			expectedQuery: url.Values{
				"location":             {"70100658"},
				recipes.HelpQueryParam: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.request, nil)
			mux.ServeHTTP(response, request)

			require.Equal(t, http.StatusFound, response.Code)
			location, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "/recipes", location.Path)
			require.Equal(t, tt.expectedQuery, location.Query())
		})
	}
}

func TestCampaignRoutesOnlyAcceptGET(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/c/issaquah", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}
