package users

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"careme/internal/cache"
)

type User struct {
	ID          string    `json:"id"`
	Email       []string  `json:"email"`
	CreatedAt   time.Time `json:"created_at"`
	LastRecipes []string  `json:"last_recipes,omitempty"`
}

type Storage struct {
	cache cache.Cache
}

var (
	ErrNotFound = errors.New("user not found")
)

const CookieName = "careme_user"

func NewStorage(c cache.Cache) *Storage {
	return &Storage{cache: c}
}

func (s *Storage) GetByID(id string) (*User, error) {

	userBytes, found := s.cache.Get(id)
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
	id, found := s.cache.Get(normalized)

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
		ID:        newUserID(),
		Email:     []string{normalizeEmail(email)},
		CreatedAt: time.Now(),
	}
	userBytes, err := json.Marshal(newUser)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new user: %w", err)
	}
	//no transactions
	if err := s.cache.Set(newUser.ID, string(userBytes)); err != nil {
		return nil, fmt.Errorf("failed to store new user: %w", err)
	}
	if err := s.cache.Set(newUser.Email[0], newUser.ID); err != nil {
		return nil, fmt.Errorf("failed to index new user by email: %w", err)
	}
	return &newUser, nil
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func newUserID() string {
	b := make([]byte, 16) //guid instead?
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("failed to generate user id: %w", err))
	}
	return hex.EncodeToString(b)
}
