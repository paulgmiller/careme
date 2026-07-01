package campaigns

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIssaquahRedirect(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		expected string
	}{
		{
			name:     "sets campaign location",
			request:  "/c/issaquah",
			expected: "/recipes?location=70100658",
		},
		{
			name:     "preserves attribution parameters",
			request:  "/c/issaquah?utm_source=facebook&utm_campaign=carts",
			expected: "/recipes?location=70100658&utm_campaign=carts&utm_source=facebook",
		},
		{
			name:     "overrides incoming location",
			request:  "/c/issaquah?location=other&utm_source=facebook",
			expected: "/recipes?location=70100658&utm_source=facebook",
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
			require.Equal(t, tt.expected, response.Header().Get("Location"))
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
