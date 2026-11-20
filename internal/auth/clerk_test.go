package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/config"
	"careme/internal/templates"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/clerktest"
	"github.com/clerk/clerk-sdk-go/v2/jwks"
	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

func TestExpiredSessionPreservesPostForSignedOutHandler(t *testing.T) {
	token, publicKey := clerktest.GenerateJWT(t, map[string]any{
		"sub": "user_expired", "sid": "sess_expired", "exp": time.Now().Add(-time.Hour).Unix(),
	}, t.Name())
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: publicKey, KeyID: t.Name(), Algorithm: "RS256", Use: "sig"}}}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/jwks", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(keys))
	}))
	t.Cleanup(api.Close)
	client := &clerkClient{jwksClient: jwks.NewClient(&clerk.ClientConfig{BackendConfig: clerk.BackendConfig{
		URL: &api.URL, HTTPClient: api.Client(), Key: new("sk_test"),
	}})}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := client.WithAuthHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				_, signedIn := client.GetUserIDFromRequest(r)
				require.False(t, signedIn)
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "Can I swap the protein?", r.FormValue("question"))
				w.WriteHeader(http.StatusUnauthorized)
			}))
			r := httptest.NewRequest(method, "/recipe/hash/question", strings.NewReader(url.Values{"question": {"Can I swap the protein?"}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("HX-Request", "true")
			r.AddCookie(&http.Cookie{Name: "__session", Value: token})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if method == http.MethodPost {
				require.True(t, called)
				require.Equal(t, http.StatusUnauthorized, w.Code)
				require.Empty(t, w.Header().Get("Location"))
			} else {
				require.False(t, called)
				require.Equal(t, http.StatusFound, w.Code)
				require.Equal(t, "/recipe/hash/question", w.Header().Get("Location"))
			}
			require.Len(t, w.Result().Cookies(), 3)
			for _, cookie := range w.Result().Cookies() {
				require.Negative(t, cookie.MaxAge)
			}
		})
	}
}

func TestGetUserIDFromRequest(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		withClaims bool
		claims     *clerk.SessionClaims
		wantID     string
		wantOK     bool
	}{
		{name: "no claims"},
		{name: "nil claims", withClaims: true},
		{
			name:       "signed in",
			withClaims: true,
			claims:     &clerk.SessionClaims{RegisteredClaims: clerk.RegisteredClaims{Subject: "user_123"}},
			wantID:     "user_123",
			wantOK:     true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.withClaims {
				r = r.WithContext(clerk.ContextWithSessionClaims(r.Context(), tt.claims))
			}
			client := &clerkClient{}
			id, ok := client.GetUserIDFromRequest(r)
			require.Equal(t, tt.wantID, id)
			require.Equal(t, tt.wantOK, ok)
		})
	}
}

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
