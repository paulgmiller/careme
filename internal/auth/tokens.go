package auth

import (
	"careme/internal/cache"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// TokenStorage manages magic link tokens
type TokenStorage struct {
	cache cache.Cache
}

const (
	tokenPrefix = "auth_token/"
	tokenLength = 32
	tokenExpiry = 15 * time.Minute
)

var ErrTokenNotFound = errors.New("token not found or expired")

type tokenData struct {
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
}

func NewTokenStorage(c cache.Cache) *TokenStorage {
	return &TokenStorage{cache: c}
}

// GenerateToken creates a new magic link token for the given email
func (ts *TokenStorage) GenerateToken(ctx context.Context, email string) (string, error) {
	// Generate a random token
	tokenBytes := make([]byte, tokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Store the token data
	now := time.Now()
	data := tokenData{
		Email:     email,
		CreatedAt: now,
		ExpiresAt: now.Add(tokenExpiry),
		Used:      false,
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token data: %w", err)
	}

	key := tokenPrefix + token
	if err := ts.cache.Set(ctx, key, string(dataBytes)); err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

// ValidateToken checks if a token is valid and returns the associated email
func (ts *TokenStorage) ValidateToken(ctx context.Context, token string) (string, error) {
	key := tokenPrefix + token
	dataReader, err := ts.cache.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return "", ErrTokenNotFound
		}
		return "", fmt.Errorf("failed to retrieve token: %w", err)
	}
	defer dataReader.Close()

	// Parse the token data
	var data tokenData
	if err := json.NewDecoder(dataReader).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to decode token data: %w", err)
	}

	// Check if token has expired
	if time.Now().After(data.ExpiresAt) {
		return "", ErrTokenNotFound
	}

	// Check if token has been used
	if data.Used {
		return "", ErrTokenNotFound
	}

	// Mark token as used (one-time use)
	data.Used = true
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal used token data: %w", err)
	}
	if err := ts.cache.Set(ctx, key, string(dataBytes)); err != nil {
		return "", fmt.Errorf("failed to mark token as used: %w", err)
	}

	return data.Email, nil
}
