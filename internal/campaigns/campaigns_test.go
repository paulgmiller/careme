package campaigns

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"careme/internal/auth"
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
				"location":           {"70100658"},
				recipes.QueryArgHelp: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
			},
		},
		{
			name:    "preserves attribution parameters",
			request: "/c/issaquah?utm_source=facebook&utm_campaign=carts",
			expectedQuery: url.Values{
				"location":           {"70100658"},
				recipes.QueryArgHelp: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
				"utm_source":         {"facebook"},
				"utm_campaign":       {"carts"},
			},
		},
		{
			name:    "overrides incoming location",
			request: "/c/issaquah?location=other&utm_source=facebook",
			expectedQuery: url.Values{
				"location":           {"70100658"},
				recipes.QueryArgHelp: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
				"utm_source":         {"facebook"},
			},
		},
		{
			name:    "overrides incoming help",
			request: "/c/issaquah?help=Custom+note",
			expectedQuery: url.Values{
				"location":           {"70100658"},
				recipes.QueryArgHelp: {AdvertisedRecipeLocations()["issaquah"].HelpMessage},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

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

func TestBellevueRedirectSetsCampaignHelp(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/c/bellevue", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusFound, response.Code)
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/recipes", location.Path)
	require.Equal(t, "70100023", location.Query().Get("location"))
	require.Equal(t, AdvertisedRecipeLocations()["bellevue"].HelpMessage, location.Query().Get(recipes.QueryArgHelp))
}

func TestCampaignRoutesOnlyAcceptGET(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/c/issaquah", nil)
	mux.ServeHTTP(response, request)

	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestWestlakeWholeFoodsRedirect(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

	for _, query := range []string{"", "?location=other&help=Custom+note&utm_source=facebook"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/westlake_wf"+query, nil))

			require.Equal(t, http.StatusFound, response.Code)
			location, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "/recipes", location.Path)
			expectedQuery := url.Values{
				"location":           {"wholefoods_10216"},
				recipes.QueryArgHelp: {genericLocationHelp("Westlake Whole Foods")},
			}
			if query != "" {
				expectedQuery.Set("utm_source", "facebook")
			}
			require.Equal(t, expectedQuery, location.Query())
		})
	}
}

func TestDeliveryCampaignRedirects(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

	for _, campaign := range []struct {
		slug       string
		locationID string
	}{
		{"smithbrothersfarms", "smithbrothersfarms_delivery"},
		{"mnfoodclub", "mnfoodclub_delivery"},
	} {
		t.Run(campaign.slug, func(t *testing.T) {
			for _, query := range []string{"", "?location=other&utm_source=facebook&utm_campaign=delivery"} {
				t.Run(query, func(t *testing.T) {
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/"+campaign.slug+query, nil))

					require.Equal(t, http.StatusFound, response.Code)
					location, err := url.Parse(response.Header().Get("Location"))
					require.NoError(t, err)
					require.Equal(t, "/recipes", location.Path)
					expectedQuery := url.Values{"location": {campaign.locationID}}
					if query != "" {
						expectedQuery.Set("utm_source", "facebook")
						expectedQuery.Set("utm_campaign", "delivery")
					}
					require.Equal(t, expectedQuery, location.Query())
				})
			}
		})
	}
}

func TestRedmondWholeFoodsCampaignRemoved(t *testing.T) {
	require.NotContains(t, AdvertisedRecipeLocations(), "redmond_wf")
	mux := http.NewServeMux()
	Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/redmond_wf", nil))
	require.Equal(t, http.StatusNotFound, response.Code)
}
