package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/session"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

var (
	ErrNoSession = errors.New("no valid session found")
)

// Client wraps Clerk SDK functionality
// todo private
type clerkClient struct {
	secretKey string
}

type AuthClient interface {
	GetUserEmail(ctx context.Context, clerkUserID string) (string, error)
	GetUserIDFromRequest(r *http.Request) (string, error)
}

var _ AuthClient = (*clerkClient)(nil)

// NewClient creates a new Clerk client wrapper
func NewClient(secretKey string) (*clerkClient, error) {
	if secretKey == "" {
		return nil, fmt.Errorf("clerk secret key is required")
	}

	// Set the global Clerk secret key
	//TODO use a local client instead? GLOBALS BAD
	clerk.SetKey(secretKey)

	return &clerkClient{
		secretKey: secretKey,
	}, nil
}

func (c *clerkClient) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	clerkUser, err := user.Get(ctx, clerkUserID)
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
		http.Redirect(w, r, r.RequestURI, http.StatusFound)
	}))

	useSessionCookie := clerkhttp.AuthorizationJWTExtractor(func(r *http.Request) string {

		if c, err := r.Cookie("__session"); err == nil {
			return c.Value
		}
		return ""
	})

	wrapped := clerkhttp.WithHeaderAuthorization(purgeAndRedirect, useSessionCookie)(handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)
	})
}

func (c *clerkClient) Logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := clerk.SessionClaimsFromContext(r.Context())
	if ok && claims.SessionID != "" {
		// Revoke the active Clerk session (sid claim).
		_, _ = session.Revoke(r.Context(), &session.RevokeParams{ID: claims.SessionID})
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

/* Toss this in if you're confused :)
func debugAuth(next http.Handler) http.Handler {
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
