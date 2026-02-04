package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkhttp "github.com/clerk/clerk-sdk-go/v2/http"
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
	//use a local client instead?
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

// GetUser retrieves a user by their Clerk user ID
/*func (c *Client) GetUser(ctx context.Context, userID string) (*clerk.User, error) {
	u, err := user.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	return u, nil
}*/

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
