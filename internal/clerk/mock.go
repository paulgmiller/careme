package auth

import (
	"careme/internal/config"
	"context"
	"net/http"
)

// Client wraps Clerk SDK functionality
type mockClient struct {
	email string
}

var _ AuthClient = (*mockClient)(nil)

// NewClient creates a new Clerk client wrapper
func Mock(cfg *config.Config) AuthClient {
	email := cfg.Mocks.Email
	if email == "" {
		email = "you@careme.cooking"
	}

	return &mockClient{
		email: email,
	}
}

func (c *mockClient) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	return c.email, nil
}

func (c *mockClient) GetUserIDFromRequest(r *http.Request) (string, error) {
	return "mock-clerk-user-id", nil
}
