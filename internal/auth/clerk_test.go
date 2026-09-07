package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/config"
	"careme/internal/templates"

	"github.com/stretchr/testify/require"
)

func TestSignInURLUsesConfiguredPublicOrigin(t *testing.T) {
	client := &clerkClient{
		cfg: &config.Config{
			Clerk:        config.ClerkConfig{Domain: "clerk.example.test"},
			PublicOrigin: "https://configured.careme.test/",
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/sign-in", nil)

	redirectURL := clerkRedirectURL(t, client.signInURL(req, false))

	require.Equal(t, "https://configured.careme.test/auth/establish", redirectURL)
}

func TestSignInURLDerivesPublicOriginFromForwardedRequest(t *testing.T) {
	client := &clerkClient{
		cfg: &config.Config{
			Clerk: config.ClerkConfig{Domain: "clerk.example.test"},
		},
	}
	returnTo := "/recipes/current?day=tuesday"
	encodedReturnTo := base64.RawURLEncoding.EncodeToString([]byte(returnTo))
	req := httptest.NewRequest(http.MethodGet, "/sign-in?return_to_b64="+url.QueryEscape(encodedReturnTo), nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "test.careme.cooking")

	redirectURL := clerkRedirectURL(t, client.signInURL(req, false))
	parsed, err := url.Parse(redirectURL)
	require.NoError(t, err)

	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "test.careme.cooking", parsed.Host)
	require.Equal(t, "/auth/establish", parsed.Path)
	require.Equal(t, encodedReturnTo, parsed.Query().Get("return_to_b64"))
}

func TestSignInURLFallsBackToLocalhostForLocalRequests(t *testing.T) {
	client := &clerkClient{
		cfg: &config.Config{
			Clerk: config.ClerkConfig{Domain: "clerk.example.test"},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/sign-in", nil)
	req.Host = ""
	req.URL.Scheme = ""
	req.URL.Host = ""

	redirectURL := clerkRedirectURL(t, client.signInURL(req, false))

	require.Equal(t, "http://localhost:8080/auth/establish", redirectURL)
}

func TestAccountRequiredPageExplainsReasonAndPreservesReturnTo(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	client := &clerkClient{cfg: &config.Config{}}
	mux := http.NewServeMux()
	client.Register(mux)

	tests := []struct {
		name    string
		reason  AccountRequiredReason
		message string
	}{
		{
			name:    "generation limit",
			reason:  AccountRequiredGenerationLimit,
			message: "two free recipe builds",
		},
		{
			name:    "add recipe",
			reason:  AccountRequiredAddRecipe,
			message: "add recipes to your kitchen and keep them for later",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			returnTo := "/recipes?h=shopping-list"
			req := httptest.NewRequest(http.MethodGet, AccountRequiredPath(tt.reason, returnTo), nil)
			rr := httptest.NewRecorder()

			mux.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			require.Contains(t, rr.Body.String(), tt.message)
			require.Contains(t, rr.Body.String(), authPath("/sign-in", returnTo))
			require.Contains(t, rr.Body.String(), authPath("/sign-up", returnTo))
		})
	}
}

func TestAccountRequiredPageRejectsInvalidInput(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	client := &clerkClient{cfg: &config.Config{}}
	mux := http.NewServeMux()
	client.Register(mux)

	tests := []string{
		"/account-required?reason=unknown&return_to_b64=" + base64.RawURLEncoding.EncodeToString([]byte("/recipes")),
		"/account-required?reason=generation-limit&return_to_b64=" + base64.RawURLEncoding.EncodeToString([]byte("https://example.com")),
	}
	for _, target := range tests {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rr := httptest.NewRecorder()

		mux.ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.False(t, strings.Contains(rr.Body.String(), "Sign in"))
	}
}

func clerkRedirectURL(t *testing.T, signInURL string) string {
	t.Helper()

	parsed, err := url.Parse(signInURL)
	require.NoError(t, err)
	return parsed.Query().Get("redirect_url")
}
