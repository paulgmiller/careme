package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"careme/internal/config"
	"careme/internal/routing"
)

// Client wraps Clerk SDK functionality
type mockClient struct {
	email      string
	userExists UserExistsFunc
}

var _ AuthClient = (*mockClient)(nil)

// NewClient creates a new Clerk client wrapper
func Mock(cfg *config.Config, userExists UserExistsFunc) AuthClient {
	email := cfg.Mocks.Email
	if email == "" {
		email = "you@careme.cooking"
	}

	return &mockClient{
		email:      email,
		userExists: userExists,
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

func (c *mockClient) Register(mux routing.Registrar) {
	mux.HandleFunc("/logout", c.logout)
	mux.HandleFunc("POST /auth/user-exists", func(w http.ResponseWriter, r *http.Request) {
		if c.userExists == nil {
			http.Error(w, "user exists handler missing", http.StatusInternalServerError)
			return
		}
		exists, err := c.userExists(r.Context(), "mock-clerk-user-id")
		if err != nil {
			http.Error(w, "unable to check account", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Exists bool `json:"exists"`
		}{
			Exists: exists,
		})
	})
}
