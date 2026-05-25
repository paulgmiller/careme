package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"careme/internal/config"

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

func clerkRedirectURL(t *testing.T, signInURL string) string {
	t.Helper()

	parsed, err := url.Parse(signInURL)
	require.NoError(t, err)
	return parsed.Query().Get("redirect_url")
}
