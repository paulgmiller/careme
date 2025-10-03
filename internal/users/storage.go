package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"careme/internal/cache"

	"github.com/google/uuid"
)

type User struct {
	ID            string    `json:"id"`
	Email         []string  `json:"email"`
	CreatedAt     time.Time `json:"created_at"`
	LastRecipes   []string  `json:"last_recipes,omitempty"`
	FavoriteStore string    `json:"favorite_store,omitempty"`
	ShoppingDay   string    `json:"shopping_day,omitempty"`
}

type Storage struct {
	cache cache.Cache
}

var (
	ErrNotFound = errors.New("user not found")
)

const (
	CookieName  = "careme_user"
	userPrefix  = "users/"
	emailPrefix = "email2user/"
)

func NewStorage(c cache.Cache) *Storage {
	return &Storage{cache: c}
}

func (s *Storage) GetByID(id string) (*User, error) {

	userBytes, found := s.cache.Get(userPrefix + id)
	if !found {
		return nil, ErrNotFound
	}
	var user User
	if err := json.Unmarshal([]byte(userBytes), &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetByEmail(email string) (*User, error) {

	normalized := normalizeEmail(email)
	id, found := s.cache.Get(emailPrefix + normalized)

	if !found {
		return nil, ErrNotFound
	}
	return s.GetByID(id)
}

func (s *Storage) FindOrCreateByEmail(email string) (*User, error) {
	user, err := s.GetByEmail(email)
	if err == nil {
		return user, nil
	}

	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	newUser := User{
		ID:        uuid.New().String(),
		Email:     []string{normalizeEmail(email)},
		CreatedAt: time.Now(),
	}
	if err := s.Update(&newUser); err != nil {
		return nil, fmt.Errorf("failed to create new user: %w", err)
	}
	if err := s.cache.Set(emailPrefix+newUser.Email[0], newUser.ID); err != nil {
		return nil, fmt.Errorf("failed to index new user by email: %w", err)
	}
	return &newUser, nil
}

func (s *Storage) Update(user *User) error {
	userBytes, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}
	if err := s.cache.Set(userPrefix+user.ID, string(userBytes)); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func normalizeEmail(email string) string {
	//remove . from before @?
	return strings.TrimSpace(strings.ToLower(email))
}
