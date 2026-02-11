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
		return DefaultMock()
	}

	return &mockClient{
		email: email,
	}
}

func DefaultMock() AuthClient {
	return &mockClient{
		email: "you@careme.cooking",
	}
}

func (c *mockClient) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	return c.email, nil
}

func (c *mockClient) GetUserIDFromRequest(r *http.Request) (string, error) {
	return "mock-clerk-user-id", nil
}

func (c *mockClient) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (c *mockClient) logout(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented in mock auth client", http.StatusNotImplemented)
}

func (c *mockClient) Register(mux *http.ServeMux) {
	mux.HandleFunc("/logout", c.logout)
}
