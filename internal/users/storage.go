package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"

	utypes "careme/internal/users/types"
)

type Storage struct {
	cache          cache.ListCache
	signupReporter SignupReporter
}

type SignupReporter interface {
	ReportSignup(ctx context.Context, user *utypes.User, r *http.Request) error
}

type StorageOption func(*Storage)

type noopSignupReporter struct{}

func (noopSignupReporter) ReportSignup(context.Context, *utypes.User, *http.Request) error {
	return nil
}

var ErrNotFound = errors.New("user not found")

const (
	CookieName  = "careme_user"
	userPrefix  = "users/"
	emailPrefix = "email2user/"
)

func NewStorage(c cache.ListCache, opts ...StorageOption) *Storage {
	storage := &Storage{
		cache:          c,
		signupReporter: noopSignupReporter{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(storage)
		}
	}
	return storage
}

func WithSignupReporter(reporter SignupReporter) StorageOption {
	return func(s *Storage) {
		if reporter != nil {
			s.signupReporter = reporter
		}
	}
}

func (s *Storage) SetSignupReporter(reporter SignupReporter) {
	if reporter == nil {
		s.signupReporter = noopSignupReporter{}
		return
	}
	s.signupReporter = reporter
}

// obviously needs to be better
func (s *Storage) List(ctx context.Context) ([]utypes.User, error) {
	userids, err := s.cache.List(ctx, userPrefix, "")
	if err != nil {
		return nil, err
	}
	var users []utypes.User
	for _, id := range userids {
		user, err := s.GetByID(id)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, nil
}

func (s *Storage) GetByID(id string) (*utypes.User, error) {
	userBytes, err := s.cache.Get(context.TODO(), userPrefix+id)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer func() {
		if err := userBytes.Close(); err != nil {
			slog.Error("failed to close user reader", "error", err)
		}
	}()
	decoder := json.NewDecoder(userBytes)

	var user utypes.User
	if err := decoder.Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetByEmail(email string) (*utypes.User, error) {
	normalized := normalizeEmail(email)
	id, err := s.cache.Get(context.TODO(), emailPrefix+normalized)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer func() {
		if err := id.Close(); err != nil {
			slog.Error("failed to close user email reader", "error", err, "email", normalized)
		}
	}()
	data, err := io.ReadAll(id)
	if err != nil {
		return nil, fmt.Errorf("failed to read user ID: %w", err)
	}
	return s.GetByID(string(data))
}

type emailFetcher interface {
	GetUserEmail(ctx context.Context, userID string) (string, error)
}

func (s *Storage) FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error) {
	user, _, err := s.EnsureFromRequest(ctx, r, authClient)
	return user, err
}

func (s *Storage) EnsureFromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, bool, error) {
	clerkUserID, err := authClient.GetUserIDFromRequest(r)
	if err != nil {
		return nil, false, err
	}
	user, created, err := s.FindOrCreateFromClerk(ctx, clerkUserID, authClient)
	if err != nil {
		return nil, false, err
	}
	if created {
		if err := s.signupReporter.ReportSignup(ctx, user, r); err != nil {
			slog.ErrorContext(ctx, "failed to report signup", "user_id", user.ID, "error", err)
		}
	}
	return user, created, nil
}

// interface for clerk client
func (s *Storage) FindOrCreateFromClerk(ctx context.Context, clerkUserID string, emailFetcher emailFetcher) (*utypes.User, bool, error) {
	user, err := s.GetByID(clerkUserID)
	if err == nil {
		return user, false, nil
	}

	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	primaryEmail, err := emailFetcher.GetUserEmail(ctx, clerkUserID)
	if err != nil {
		return nil, false, fmt.Errorf("failed to fetch user email from clerk: %w", err)
	}

	newUser := utypes.User{
		ID:          clerkUserID, // do we need this o be independent for housholds?
		Email:       []string{normalizeEmail(primaryEmail)},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
	}
	if err := s.Update(&newUser); err != nil {
		return nil, false, fmt.Errorf("failed to create new user: %w", err)
	}
	if err := s.cache.Put(context.TODO(), emailPrefix+newUser.Email[0], newUser.ID, cache.Unconditional()); err != nil {
		return nil, false, fmt.Errorf("failed to index new user by email: %w", err)
	}
	slog.InfoContext(ctx, "created new user", "id", clerkUserID, "email", primaryEmail)
	return &newUser, true, nil
}

func (s *Storage) Update(user *utypes.User) error {
	if err := user.Validate(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}

	userBytes, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}
	if err := s.cache.Put(context.TODO(), userPrefix+user.ID, string(userBytes), cache.Unconditional()); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func normalizeEmail(email string) string {
	// remove . from before @? or +<suffix?
	return strings.TrimSpace(strings.ToLower(email))
}
