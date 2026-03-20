package auth

import (
	"careme/internal/config"
	"careme/internal/routing"
	"careme/internal/templates"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/jwks"
	"github.com/clerk/clerk-sdk-go/v2/session"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/samber/lo"
)

var ErrNoSession = errors.New("no valid session found")

type AuthClient interface {
	GetUserEmail(ctx context.Context, clerkUserID string) (string, error)
	GetUserIDFromRequest(r *http.Request) (string, error)
	WithAuthHTTP(handler http.Handler) http.Handler
	Register(mux routing.Registrar)
}

// Client wraps Clerk SDK functionality
// todo private
type clerkClient struct {
	cfg           *config.Config
	userClient    *user.Client
	sessionClient *session.Client
	jwksClient    *jwks.Client
}

var _ AuthClient = (*clerkClient)(nil)

// NewClient creates a new Clerk client wrapper
func NewClient(cfg *config.Config) (*clerkClient, error) {
	if cfg.Clerk.SecretKey == "" {
		return nil, fmt.Errorf("clerk secret key is required")
	}

	clientConfig := &clerk.ClientConfig{
		BackendConfig: clerk.BackendConfig{
			Key: clerk.String(cfg.Clerk.SecretKey),
		},
	}

	return &clerkClient{
		userClient:    user.NewClient(clientConfig),
		sessionClient: session.NewClient(clientConfig),
		jwksClient:    jwks.NewClient(clientConfig),
		cfg:           cfg,
	}, nil
}

func (c *clerkClient) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	// todo can we pull this right off of claims? not woth bothering?
	clerkUser, err := c.userClient.Get(ctx, clerkUserID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch clerk user: %w", err)
	}

	// Get primary email from Clerk user.
	// do we need to rync this ever?
	var primaryEmail string
	for _, emailAddr := range clerkUser.EmailAddresses {
		if clerkUser.PrimaryEmailAddressID != nil &&
			emailAddr.ID == *clerkUser.PrimaryEmailAddressID {
			primaryEmail = emailAddr.EmailAddress
			break
		}
	}

	if primaryEmail == "" {
		return "", fmt.Errorf("no primary email found for clerk user %s", clerkUserID)
	}

	return primaryEmail, nil
}

func (c *clerkClient) GetUserIDFromRequest(r *http.Request) (string, error) {
	sessionClaims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || sessionClaims == nil {
		return "", ErrNoSession
	}
	return sessionClaims.Subject, nil
}

func (c *clerkClient) FromRequest(ctx context.Context, req *http.Request) (string, error) {
	clerkUserID, err := c.GetUserIDFromRequest(req)
	if err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "found clerk user ID", "clerk_user_id", clerkUserID)
	return clerkUserID, nil
}

// WithClerkHTTP wraps the http.Handler with Clerk's authentication middleware
func (c *clerkClient) WithAuthHTTP(handler http.Handler) http.Handler {
	purgeAndRedirect := clerkhttp.AuthorizationFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("authorization failure, purging cookies and redirecting")
		// Clear any existing Clerk cookies by setting them to expired
		clearCookie(w, "__session")
		clearCookie(w, "__clerk_db_jwt") // common in dev flows
		clearCookie(w, "__client")       // if present in your setup
		http.Redirect(w, r, r.RequestURI, http.StatusFound)
	}))

	useSessionCookie := clerkhttp.AuthorizationJWTExtractor(func(r *http.Request) string {
		if c, err := r.Cookie("__session"); err == nil {
			return c.Value
		}
		return ""
	})

	wrapped := clerkhttp.WithHeaderAuthorization(
		purgeAndRedirect,
		useSessionCookie,
		clerkhttp.JWKSClient(c.jwksClient),
	)(handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)
	})
}

func (c *clerkClient) logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if ok && claims.SessionID != "" {
		// Revoke the active Clerk session (sid claim).
		_, _ = c.sessionClient.Revoke(r.Context(), &session.RevokeParams{ID: claims.SessionID})
	}

	// Clear app-domain cookies that can re-bootstrap auth.
	clearCookie(w, "__session")
	clearCookie(w, "__clerk_db_jwt") // common in dev flows
	clearCookie(w, "__client")       // if present in your setup

	// Redirect to a logged-out page in your app.
	http.Redirect(w, r, "/", http.StatusFound)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true, // keep this true in prod
		SameSite: http.SameSiteLaxMode,
	})
}

func (c *clerkClient) Register(mux routing.Registrar) {
	mux.HandleFunc("/logout", c.logout)
	mux.HandleFunc("/sign-in", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, c.signInURL(r, false), http.StatusSeeOther)
	})
	mux.HandleFunc("/sign-up", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, c.signInURL(r, true), http.StatusSeeOther)
	})
	mux.HandleFunc("/auth/establish", func(w http.ResponseWriter, r *http.Request) {
		if c.cfg.Clerk.PublishableKey == "" {
			http.Error(w, "clerk publishable key missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := struct {
			PublishableKey      string
			GoogleTagScript     template.HTML
			GoogleConversionTag string
			Signup              bool
			ReturnTo            string // read from a data- attribute in the template to avoid JS-string escaping concerns
		}{
			PublishableKey:      c.cfg.Clerk.PublishableKey,
			GoogleTagScript:     templates.GoogleTagScript(),
			GoogleConversionTag: templates.GoogleConversionTag(),
			Signup:              strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("signup")), "true"), // used for ad conversions
			ReturnTo:            returnToFromRequest(r),
		}
		if err := templates.AuthEstablish.Execute(w, data); err != nil {
			slog.ErrorContext(r.Context(), "auth establish template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

func (c *clerkClient) signInURL(r *http.Request, signup bool) string {
	base := c.cfg.Clerk.Signin()
	if signup {
		base = c.cfg.Clerk.Signup()
	}
	redirectURL := c.authEstablishURL(signup, sanitizeReturnTo(r.URL.Query().Get("return_to")))
	u := lo.Must(url.Parse(base))
	q := u.Query()
	q.Set("redirect_url", redirectURL)
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *clerkClient) authEstablishURL(signup bool, returnTo string) string {
	origin := c.cfg.ResolvedPublicOrigin() // can never be emptpy
	u := lo.Must(url.Parse(origin + "/auth/establish"))
	q := u.Query()
	if signup {
		q.Set("signup", "true")
	}
	if returnTo != "" {
		q.Set("return_to_b64", base64.RawURLEncoding.EncodeToString([]byte(returnTo)))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func returnToFromRequest(r *http.Request) string {
	if encoded := strings.TrimSpace(r.URL.Query().Get("return_to_b64")); encoded != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err == nil {
			return sanitizeReturnTo(string(decoded))
		}
	}
	return sanitizeReturnTo(r.URL.Query().Get("return_to"))
}

func sanitizeReturnTo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return ""
	}
	return raw
}

// Toss this in if you're confused :)
/*
func DebugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		hasDB := q.Has("__clerk_db_jwt")

		authz := r.Header.Get("Authorization")
		hasAuthz := authz != ""

		// list cookie names only (don’t log values)
		cookieNames := []string{}
		for _, c := range r.Cookies() {
			cookieNames = append(cookieNames, c.Name)
		}

		log.Printf("auth-debug path=%s host=%s xf_proto=%q xf_host=%q hasAuthz=%t has__clerk_db_jwt=%t cookies=%v",
			r.URL.Path,
			r.Host,
			r.Header.Get("X-Forwarded-Proto"),
			r.Header.Get("X-Forwarded-Host"),
			hasAuthz,
			hasDB,
			cookieNames,
		)

		next.ServeHTTP(w, r)

	})
}*/
