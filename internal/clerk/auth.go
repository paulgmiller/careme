package clerk

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

var (
	ErrNoSession = errors.New("no valid session found")
)

// Client wraps Clerk SDK functionality
type Client struct {
	secretKey string
}

// NewClient creates a new Clerk client wrapper
func NewClient(secretKey string) (*Client, error) {
	if secretKey == "" {
		return nil, fmt.Errorf("clerk secret key is required")
	}

	// Set the global Clerk secret key
	clerk.SetKey(secretKey)

	return &Client{
		secretKey: secretKey,
	}, nil
}

// GetUser retrieves a user by their Clerk user ID
func (c *Client) GetUser(ctx context.Context, userID string) (*clerk.User, error) {
	u, err := user.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	return u, nil
}

// RequireAuth is middleware that requires a valid Clerk session
func (c *Client) RequireAuth(next http.Handler) http.Handler {
	return clerkhttp.RequireHeaderAuthorization()(next)
}

// GetUserIDFromRequest extracts the user ID from a Clerk session in the request context
func GetUserIDFromRequest(r *http.Request) (string, error) {
	sessionClaims, ok := clerk.SessionClaimsFromContext(r.Context())
	if !ok || sessionClaims == nil {
		return "", ErrNoSession
	}
	return sessionClaims.Subject, nil
}

// WithClerkHTTP wraps the http.Handler with Clerk's authentication middleware
func (c *Client) WithClerkHTTP(handler http.Handler) http.Handler {

	purgeAndRedirect := clerkhttp.AuthorizationFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clear any existing Clerk cookies by setting them to expired
		http.SetCookie(w, &http.Cookie{
			Name:  "__session",
			Value: "",
		})
		http.Redirect(w, r, r.RequestURI, http.StatusFound)
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Header.Get("Authorization") == "" {
			if c, err := r.Cookie("__session"); err == nil && c.Value != "" {
				r.Header.Set("Authorization", "Bearer "+c.Value)
			}
		}
		clerkhttp.WithHeaderAuthorization(purgeAndRedirect)(handler)
	})

}
