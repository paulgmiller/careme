package locations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/auth"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signedInZipUserLookup struct{}

func (signedInZipUserLookup) FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error) {
	return &utypes.User{}, nil
}

func TestLocationsPageZipCookie(t *testing.T) {
	mustInitLocationTemplates(t)

	for _, tt := range []struct {
		name       string
		query      string
		signedIn   bool
		secure     bool
		backendErr error
		wantStatus int
		wantZip    string
	}{
		{name: "signed out", query: "zip=10001", wantStatus: http.StatusOK, wantZip: "10001"},
		{name: "signed in", query: "zip=10001", signedIn: true, wantStatus: http.StatusOK, wantZip: "10001"},
		{name: "https", query: "zip=10001", secure: true, wantStatus: http.StatusOK, wantZip: "10001"},
		{name: "leading zero", query: "zip=00601", wantStatus: http.StatusOK, wantZip: "00601"},
		{name: "trim whitespace", query: "zip=+10001+", wantStatus: http.StatusOK, wantZip: "10001"},
		{name: "zip plus four", query: "zip=10001-1234", wantStatus: http.StatusOK, wantZip: "10001-1234"},
		{name: "backend failure", query: "zip=10001", backendErr: errors.New("backend unavailable"), wantStatus: http.StatusInternalServerError, wantZip: "10001"},
		{name: "coordinates", query: "lat=47.6&lon=-122.3", wantStatus: http.StatusOK},
		{name: "invalid zip", query: "zip=invalid", wantStatus: http.StatusBadRequest},
		{name: "unknown zip", query: "zip=00000", wantStatus: http.StatusBadRequest},
		{name: "mixed inputs", query: "zip=10001&lat=47.6&lon=-122.3", wantStatus: http.StatusBadRequest},
		{name: "missing zip", query: "zip=", wantStatus: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeLocationClient()
			client.err = tt.backendErr
			var users userLookup = fakeUserLookup{}
			if tt.signedIn {
				users = signedInZipUserLookup{}
			}
			server := NewServer(newTestLocationServer(client), LoadCentroids(), users, fakeProduceScoreLookup{})
			mux := http.NewServeMux()
			server.Register(mux, auth.DefaultMock())

			scheme := "http"
			if tt.secure {
				scheme = "https"
			}
			req := httptest.NewRequest(http.MethodGet, scheme+"://careme.test/locations?"+tt.query, nil)
			req.AddCookie(&http.Cookie{Name: zipCodeCookieName, Value: "98005"})
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code, rr.Body.String())
			cookie := findResponseCookie(rr, zipCodeCookieName)
			if tt.wantZip == "" {
				assert.Nil(t, cookie, "search must preserve the previously saved ZIP")
				return
			}
			require.NotNil(t, cookie)
			assert.Equal(t, tt.wantZip, cookie.Value)
			assert.Equal(t, "/", cookie.Path)
			assert.Equal(t, 90*24*60*60, cookie.MaxAge)
			assert.True(t, cookie.HttpOnly)
			assert.Equal(t, tt.secure, cookie.Secure)
			assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		})
	}
}
