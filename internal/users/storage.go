package users

import (
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"
)

var daysOfWeek = [...]string{
	time.Sunday.String(),
	time.Monday.String(),
	time.Tuesday.String(),
	time.Wednesday.String(),
	time.Thursday.String(),
	time.Friday.String(),
	time.Saturday.String(),
}

func parseWeekday(v string) (time.Weekday, error) {
	for i := range daysOfWeek {
		if strings.EqualFold(daysOfWeek[i], v) {
			return time.Weekday(i), nil
		}
	}

	return time.Sunday, fmt.Errorf("invalid weekday '%s'", v)
}

type Recipe struct {
	Title     string    `json:"id"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID            string    `json:"id"`
	Email         []string  `json:"email"`
	CreatedAt     time.Time `json:"created_at"`
	LastRecipes   []Recipe  `json:"last_recipes,omitempty"`
	FavoriteStore string    `json:"favorite_store,omitempty"`
	ShoppingDay   string    `json:"shopping_day,omitempty"`
}

// need to take a look up to location cache?
func (u *User) Validate() error {
	if _, err := parseWeekday(u.ShoppingDay); err != nil {
		return err
	}
	if len(u.Email) == 0 {
		return errors.New("at least one email is required")
	}
	for _, e := range u.Email {
		if _, err := mail.ParseAddress(e); err != nil {
			return errors.New("invalid email address: " + e)
		}
	}
	if u.FavoriteStore != "" {
		if _, err := strconv.Atoi(u.FavoriteStore); err != nil {
			return fmt.Errorf("invalid favorite store id %s: %w", u.FavoriteStore, err)
		}
	}
	// trim out recipes older than 2 months? store them in seperate file?
	slices.SortFunc(u.LastRecipes, func(a, b Recipe) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return nil
}

type Storage struct {
	cache cache.ListCache
}

var ErrNotFound = errors.New("user not found")

const (
	CookieName  = "careme_user"
	userPrefix  = "users/"
	emailPrefix = "email2user/"
)

func NewStorage(c cache.ListCache) *Storage {
	return &Storage{cache: c}
}

// obviously needs to be better
func (s *Storage) List(ctx context.Context) ([]User, error) {
	userids, err := s.cache.List(ctx, userPrefix, "")
	if err != nil {
		return nil, err
	}
	var users []User
	for _, id := range userids {
		slog.InfoContext(ctx, "loading user", "id", id)
		user, err := s.GetByID(id)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, nil
}

func (s *Storage) GetByID(id string) (*User, error) {
	userBytes, err := s.cache.Get(context.TODO(), userPrefix+id)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer func() {
		if err := userBytes.Close(); err != nil {
			slog.Error("failed to close user reader", "error", err, "user_id", id)
		}
	}()
	decoder := json.NewDecoder(userBytes)

	var user User
	if err := decoder.Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetByEmail(email string) (*User, error) {
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

// interface for clerk client
func (s *Storage) FindOrCreateFromClerk(ctx context.Context, clerkUserID string, emailFetcher emailFetcher) (*User, error) {
	user, err := s.GetByID(clerkUserID)
	if err == nil {
		return user, nil
	}

	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	primaryEmail, err := emailFetcher.GetUserEmail(ctx, clerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user email from clerk: %w", err)
	}

	newUser := User{
		ID:          clerkUserID, //do we need this o be independent for housholds?
		Email:       []string{normalizeEmail(primaryEmail)},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
	}
	if err := s.Update(&newUser); err != nil {
		return nil, fmt.Errorf("failed to create new user: %w", err)
	}
	if err := s.cache.Put(context.TODO(), emailPrefix+newUser.Email[0], newUser.ID, cache.Unconditional()); err != nil {
		return nil, fmt.Errorf("failed to index new user by email: %w", err)
	}
	slog.InfoContext(ctx, "created new user", "id", clerkUserID, "email", primaryEmail)
	return &newUser, nil
}

func (s *Storage) Update(user *User) error {
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
	// remove . from before @?
	return strings.TrimSpace(strings.ToLower(email))
}
