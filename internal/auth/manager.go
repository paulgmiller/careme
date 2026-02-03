package auth

import (
	"careme/internal/config"
	"careme/internal/users"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
)

const clerkSessionCookie = "__session"

type Manager struct {
	cfg        *config.Config
	store      *users.Storage
	client     *clerk.Client
	useOffline bool
}

func NewManager(cfg *config.Config, store *users.Storage) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("auth config is required")
	}
	if store == nil {
		return nil, errors.New("user storage is required")
	}

	manager := &Manager{
		cfg:   cfg,
		store: store,
	}

	if cfg.Mocks.Enable && cfg.Clerk.SecretKey == "" {
		manager.useOffline = true
		return manager, nil
	}

	client, err := clerk.NewClient(clerk.WithSecretKey(cfg.Clerk.SecretKey))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize clerk client: %w", err)
	}

	manager.client = client
	return manager, nil
}

func (m *Manager) Offline() bool {
	return m.useOffline
}

func (m *Manager) SignInURL() string {
	return m.cfg.Clerk.SignInURL
}

func (m *Manager) SignUpURL() string {
	return m.cfg.Clerk.SignUpURL
}

func (m *Manager) SignOutURL() string {
	return m.cfg.Clerk.SignOutURL
}

func (m *Manager) CurrentUser(r *http.Request) (*users.User, error) {
	if m.useOffline {
		return users.FromRequest(r, m.store)
	}

	token := sessionTokenFromRequest(r)
	if token == "" {
		return nil, users.ErrNotFound
	}

	claims, err := clerk.VerifyToken(token, &clerk.VerifyTokenOptions{SecretKey: m.cfg.Clerk.SecretKey})
	if err != nil {
		return nil, err
	}
	userID := claims.Subject
	if userID == "" {
		return nil, users.ErrNotFound
	}

	email := claims.Email
	if email == "" {
		email, err = m.fetchPrimaryEmail(r.Context(), userID)
		if err != nil {
			return nil, err
		}
	}

	return m.store.FindOrCreateByID(userID, email)
}

func (m *Manager) fetchPrimaryEmail(ctx context.Context, userID string) (string, error) {
	if m.client == nil {
		return "", errors.New("clerk client unavailable")
	}
	user, err := m.client.Users.Get(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to load clerk user: %w", err)
	}
	if user.PrimaryEmailAddressID != "" {
		for _, email := range user.EmailAddresses {
			if email.ID == user.PrimaryEmailAddressID {
				return email.EmailAddress, nil
			}
		}
	}
	if len(user.EmailAddresses) > 0 {
		return user.EmailAddresses[0].EmailAddress, nil
	}
	return "", errors.New("clerk user missing email address")
}

func sessionTokenFromRequest(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	if cookie, err := r.Cookie(clerkSessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}
