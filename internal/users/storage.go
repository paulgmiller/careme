package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"careme/internal/cache"

	"github.com/google/uuid"
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

// GetFavoriteStore returns the user's favorite store ID
func (u *User) GetFavoriteStore() string {
	return u.FavoriteStore
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
	//trim out recipes older than 2 months?

	return nil
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

	userBytes, err := s.cache.Get(userPrefix + id)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer userBytes.Close()
	decoder := json.NewDecoder(userBytes)

	var user User
	if err := decoder.Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetByEmail(email string) (*User, error) {

	normalized := normalizeEmail(email)
	id, err := s.cache.Get(emailPrefix + normalized)

	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer id.Close()
	data, err := io.ReadAll(id)
	if err != nil {
		return nil, fmt.Errorf("failed to read user ID: %w", err)
	}
	return s.GetByID(string(data))
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
		ID:          uuid.New().String(),
		Email:       []string{normalizeEmail(email)},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
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
	if err := user.Validate(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}

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
