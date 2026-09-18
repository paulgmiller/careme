package campaigns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
		{"landing subpath", http.MethodGet, "/c/fancy/extra", auth.ErrNoSession, http.StatusNotFound},
		{"unknown campaign", http.MethodGet, "/c/missing", auth.ErrNoSession, http.StatusNotFound},
		{"old URL", http.MethodGet, "/campaigns/fancy", auth.ErrNoSession, http.StatusNotFound},
		{"POST rejected", http.MethodPost, "/c/fancy", auth.ErrNoSession, http.StatusMethodNotAllowed},
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
				assert.Contains(t, response.Body.String(), c.Instructions)
				assert.Contains(t, response.Body.String(), "Use your location")
				assert.NotContains(t, response.Body.String(), "Your kitchen")
			}
		})
	}
}
