package auth

import (
	"careme/internal/cache"
	"context"
	"testing"
)

func TestTokenGeneration(t *testing.T) {
	c := cache.NewFileCache("/tmp/test-auth-cache")
	storage := NewTokenStorage(c)
	ctx := context.Background()

	email := "test@example.com"
	token, err := storage.GenerateToken(ctx, email)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestTokenValidation(t *testing.T) {
	c := cache.NewFileCache("/tmp/test-auth-cache")
	storage := NewTokenStorage(c)
	ctx := context.Background()

	email := "test@example.com"
	token, err := storage.GenerateToken(ctx, email)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Validate the token
	retrievedEmail, err := storage.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if retrievedEmail != email {
		t.Fatalf("expected email %s, got %s", email, retrievedEmail)
	}
}

func TestTokenOneTimeUse(t *testing.T) {
	c := cache.NewFileCache("/tmp/test-auth-cache")
	storage := NewTokenStorage(c)
	ctx := context.Background()

	email := "test@example.com"
	token, err := storage.GenerateToken(ctx, email)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Validate the token once
	_, err = storage.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	// Try to validate again - should fail
	_, err = storage.ValidateToken(ctx, token)
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound on second validation, got %v", err)
	}
}

func TestTokenExpiry(t *testing.T) {
	t.Skip("Skipping expiry test as it would require waiting 15 minutes or manipulating time")
	// In a real implementation, you might want to:
	// 1. Make tokenExpiry configurable for testing
	// 2. Use a time manipulation library
	// 3. Test the logic separately from actual timing
}

func TestInvalidToken(t *testing.T) {
	c := cache.NewFileCache("/tmp/test-auth-cache")
	storage := NewTokenStorage(c)
	ctx := context.Background()

	// Try to validate a token that doesn't exist
	_, err := storage.ValidateToken(ctx, "invalid-token-12345")
	if err != ErrTokenNotFound {
		t.Fatalf("expected ErrTokenNotFound for invalid token, got %v", err)
	}
}
