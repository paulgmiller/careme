package users

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type storageFile struct {
	Users []User `json:"users"`
}

type Storage struct {
	path string
	mu   sync.Mutex
}

var (
	ErrNotFound = errors.New("user not found")
)

const CookieName = "careme_user"

func NewStorage(path string) *Storage {
	return &Storage{path: path}
}

func (s *Storage) GetByID(id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, user := range users.Users {
		if user.ID == id {
			u := user
			return &u, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Storage) GetByEmail(email string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeEmail(email)
	users, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, user := range users.Users {
		if user.Email == normalized {
			u := user
			return &u, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Storage) FindOrCreateByEmail(email string) (*User, error) {
	normalized := normalizeEmail(email)
	if normalized == "" {
		return nil, fmt.Errorf("email is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := s.load()
	if err != nil {
		return nil, err
	}

	for _, user := range users.Users {
		if user.Email == normalized {
			u := user
			return &u, nil
		}
	}

	newUser := User{
		ID:        newUserID(),
		Email:     normalized,
		CreatedAt: time.Now(),
	}
	users.Users = append(users.Users, newUser)
	if err := s.save(users); err != nil {
		return nil, err
	}
	return &newUser, nil
}

func (s *Storage) load() (storageFile, error) {
	var users storageFile
	if err := s.ensureDir(); err != nil {
		return users, err
	}
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return users, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return users, fmt.Errorf("failed to read users file: %w", err)
	}
	if len(data) == 0 {
		return users, nil
	}
	if err := json.Unmarshal(data, &users); err != nil {
		return users, fmt.Errorf("failed to decode users: %w", err)
	}
	return users, nil
}

func (s *Storage) save(users storageFile) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode users: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write users file: %w", err)
	}
	return nil
}

func (s *Storage) ensureDir() error {
	return os.MkdirAll(filepath.Dir(s.path), 0o755)
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func newUserID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("failed to generate user id: %w", err))
	}
	return hex.EncodeToString(b)
}
