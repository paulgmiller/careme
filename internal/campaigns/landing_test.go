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
		{"budget landing", http.MethodGet, "/c/budget-dinners", auth.ErrNoSession, http.StatusOK},
		{"guest landing", http.MethodGet, "/c/fancy-dinners", auth.ErrNoSession, http.StatusOK},
		{"unknown campaign", http.MethodGet, "/c/missing", auth.ErrNoSession, http.StatusNotFound},
		{"old URL", http.MethodGet, "/campaigns/fancy-dinners", auth.ErrNoSession, http.StatusNotFound},
		{"POST rejected", http.MethodPost, "/c/fancy-dinners", auth.ErrNoSession, http.StatusMethodNotAllowed},
		{"account failure", http.MethodGet, "/c/fancy-dinners", errors.New("account unavailable"), http.StatusInternalServerError},
		{"existing redirect", http.MethodGet, "/c/issaquah", auth.ErrNoSession, http.StatusFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			Register(mux)
			RegisterLanding(mux, landingUserStub{err: tt.userErr}, auth.DefaultMock())
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			require.Equal(t, tt.status, response.Code)
			if tt.status == http.StatusOK {
				c := dinnerCampaigns[strings.TrimPrefix(tt.path, "/c/")]
				assert.Contains(t, response.Body.String(), c.Title)
				assert.Contains(t, response.Body.String(), c.Blurb)
				assert.Contains(t, response.Body.String(), c.Instructions)
				assert.Contains(t, response.Body.String(), "Use your location")
				assert.NotContains(t, response.Body.String(), "Your kitchen")
			}
		})
	}
}
