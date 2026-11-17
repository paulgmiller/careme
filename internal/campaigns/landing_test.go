package campaigns

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/auth"
	"careme/internal/config"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type landingUserStub struct{ err error }

func (s landingUserStub) FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error) {
	return nil, s.err
}

func TestLandingRoutes(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, tt := range []struct {
		name    string
		method  string
		path    string
		userErr error
		status  int
	}{
		{"budget landing", http.MethodGet, "/c/budget", auth.ErrNoSession, http.StatusOK},
		{"vegetarian landing", http.MethodGet, "/c/vegetarian", auth.ErrNoSession, http.StatusOK},
		{"mediterranean landing", http.MethodGet, "/c/mediterranean", auth.ErrNoSession, http.StatusOK},
		{"low-carb landing", http.MethodGet, "/c/low-carb", auth.ErrNoSession, http.StatusOK},
		{"guest landing", http.MethodGet, "/c/fancy", auth.ErrNoSession, http.StatusOK},
		{"thanksgiving landing", http.MethodGet, "/c/thanksgiving", auth.ErrNoSession, http.StatusOK},
		{"landing subpath", http.MethodGet, "/c/fancy/extra", auth.ErrNoSession, http.StatusNotFound},
		{"unknown campaign", http.MethodGet, "/c/missing", auth.ErrNoSession, http.StatusNotFound},
		{"old URL", http.MethodGet, "/campaigns/fancy", auth.ErrNoSession, http.StatusNotFound},
		{"POST rejected", http.MethodPost, "/c/fancy", auth.ErrNoSession, http.StatusMethodNotAllowed},
		{"POST plan rejected", http.MethodPost, "/c/fancy/plan", auth.ErrNoSession, http.StatusMethodNotAllowed},
		{"non-party plan", http.MethodGet, "/c/budget/plan", auth.ErrNoSession, http.StatusFound},
		{"unknown plan missing", http.MethodGet, "/c/missing/plan", auth.ErrNoSession, http.StatusNotFound},
		{"account failure", http.MethodGet, "/c/fancy", errors.New("account unavailable"), http.StatusInternalServerError},
		{"existing redirect", http.MethodGet, "/c/issaquah", auth.ErrNoSession, http.StatusFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux, landingUserStub{err: tt.userErr}, auth.DefaultMock())
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			require.Equal(t, tt.status, response.Code)
			if tt.status == http.StatusOK {
				c := dinnerCampaigns[strings.TrimPrefix(tt.path, "/c/")]
				assert.Contains(t, response.Body.String(), "<title>"+c.Title+" | Careme</title>")
				assert.Contains(t, response.Body.String(), `<meta name="description" content="`+c.Blurb+`" />`)
				assert.Contains(t, response.Body.String(), c.Blurb)
				assert.Contains(t, response.Body.String(), `action="`+tt.path+`/plan"`)
				assert.NotContains(t, response.Body.String(), `name="instructions"`)
				if c.AskGuestCount {
					assert.Contains(t, response.Body.String(), `name="guests" type="number" min="1" max="100" step="1" required`)
					assert.Contains(t, response.Body.String(), "How many people are coming?")
				} else {
					assert.NotContains(t, response.Body.String(), `name="guests"`)
				}
				assert.Contains(t, response.Body.String(), "Use my location")
				assert.Contains(t, response.Body.String(), `autocomplete="postal-code"`)
				assert.Contains(t, response.Body.String(), "Find nearby stores")
				if c.ImageURL != "" {
					assert.Contains(t, response.Body.String(), c.ImageURL)
					assert.Contains(t, response.Body.String(), `alt="`+html.EscapeString(c.ImageAlt)+`"`)
					assert.NotContains(t, response.Body.String(), "Dinner photo coming soon")
				} else {
					assert.Contains(t, response.Body.String(), "Dinner photo coming soon")
				}
				if tt.path == "/c/thanksgiving" {
					assert.Contains(t, response.Body.String(), `src="https://test.careme.cooking/recipe/lqi74zXNvQNoFdRTrIBD2g==/image"`)
					assert.Contains(t, response.Body.String(), `alt="A slice of pumpkin pie topped with whipped cream."`)
				}
				assert.NotContains(t, response.Body.String(), "Your kitchen")
			}
		})
	}
}

func TestLandingWithLocationQueryStillRenders(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, slug := range []string{"fancy", "thanksgiving"} {
		for _, query := range []string{"zip=98101&guests=8", "lat=47.6&lon=-122.3&guests=0"} {
			t.Run(slug+"/"+query, func(t *testing.T) {
				mux := http.NewServeMux()
				Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/"+slug+"?"+query, nil))
				require.Equal(t, http.StatusOK, response.Code)
				assert.Empty(t, response.Header().Get("Location"))
				assert.Contains(t, response.Body.String(), `action="/c/`+slug+`/plan"`)
			})
		}
	}
}

func TestGuestCountSubmission(t *testing.T) {
	for _, slug := range []string{"fancy", "thanksgiving"} {
		for _, location := range []string{"zip=98101", "lat=47.6&lon=-122.3"} {
			for _, guests := range []string{"1", "8", "100", "", "0", "-1", "101", "2.5", "lots"} {
				t.Run(slug+"/"+location+"/"+guests, func(t *testing.T) {
					mux := http.NewServeMux()
					Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/"+slug+"/plan?"+location+"&guests="+guests+"&instructions=override&utm_source=facebook", nil))
					if guests != "1" && guests != "8" && guests != "100" {
						require.Equal(t, http.StatusBadRequest, response.Code)
						assert.Empty(t, response.Header().Get("Location"))
						return
					}
					require.Equal(t, http.StatusFound, response.Code)
					target, err := url.Parse(response.Header().Get("Location"))
					require.NoError(t, err)
					assert.Equal(t, "/locations", target.Path)
					query := target.Query()
					assert.Equal(t, dinnerCampaigns[slug].Instructions+"\nCook for "+guests+" people. Use this as the guest count when planning portions.", query.Get("instructions"))
					assert.Equal(t, "facebook", query.Get("utm_source"))
					assert.False(t, query.Has("guests"))
					coordinates, err := url.ParseQuery(location)
					require.NoError(t, err)
					for key := range coordinates {
						assert.Equal(t, coordinates.Get(key), query.Get(key))
					}
				})
			}
		}
	}
}

func TestPlanWithoutGuestCount(t *testing.T) {
	for slug, campaign := range dinnerCampaigns {
		if campaign.AskGuestCount {
			continue
		}
		for _, location := range []string{"zip=98101", "lat=47.6&lon=-122.3"} {
			t.Run(slug+"/"+location, func(t *testing.T) {
				mux := http.NewServeMux()
				Register(mux, landingUserStub{err: auth.ErrNoSession}, auth.DefaultMock())
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/c/"+slug+"/plan?"+location+"&instructions=override&utm_source=facebook", nil))
				require.Equal(t, http.StatusFound, response.Code)
				target, err := url.Parse(response.Header().Get("Location"))
				require.NoError(t, err)
				assert.Equal(t, "/locations", target.Path)
				query := target.Query()
				assert.Equal(t, campaign.Instructions, query.Get("instructions"))
				assert.Equal(t, "facebook", query.Get("utm_source"))
				coordinates, err := url.ParseQuery(location)
				require.NoError(t, err)
				for key := range coordinates {
					assert.Equal(t, coordinates.Get(key), query.Get(key))
				}
			})
		}
	}
}
